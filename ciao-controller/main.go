/*
// Copyright (c) 2016 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/01org/ciao/ciao-controller/api"
	"github.com/01org/ciao/ciao-controller/internal/datastore"
	"github.com/01org/ciao/ciao-controller/internal/quotas"
	storage "github.com/01org/ciao/ciao-storage"
	"github.com/01org/ciao/clogger/gloginterface"
	"github.com/01org/ciao/database"
	"github.com/01org/ciao/openstack/block"
	"github.com/01org/ciao/openstack/compute"
	osIdentity "github.com/01org/ciao/openstack/identity"
	"github.com/01org/ciao/osprepare"
	"github.com/01org/ciao/ssntp"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
)

type tenantConfirmMemo struct {
	ch  chan struct{}
	err error
}

type controller struct {
	storage.BlockDriver
	client              controllerClient
	ds                  *datastore.Datastore
	is                  *ImageService
	id                  *identity
	apiURL              string
	tenantReadiness     map[string]*tenantConfirmMemo
	tenantReadinessLock sync.Mutex
	qs                  *quotas.Quotas
	httpServers         []*http.Server
}

var cert = flag.String("cert", "", "Client certificate")
var caCert = flag.String("cacert", "", "CA certificate")
var serverURL = flag.String("url", "", "Server URL")
var identityURL = "identity:35357"
var serviceUser = "csr"
var servicePassword = ""
var volumeAPIPort = block.APIPort
var computeAPIPort = compute.APIPort
var controllerAPIPort = api.Port
var httpsCAcert = "/etc/pki/ciao/ciao-controller-cacert.pem"
var httpsKey = "/etc/pki/ciao/ciao-controller-key.pem"
var workloadsPath = flag.String("workloads_path", "/var/lib/ciao/data/controller/workloads", "path to yaml files")
var persistentDatastoreLocation = flag.String("database_path", "/var/lib/ciao/data/controller/ciao-controller.db", "path to persistent database")
var imageDatastoreLocation = flag.String("image_database_path", "/var/lib/ciao/data/image/ciao-image.db", "path to image persistent database")
var logDir = "/var/lib/ciao/logs/controller"

var imagesPath = flag.String("images_path", "/var/lib/ciao/images", "path to ciao images")

var cephID = flag.String("ceph_id", "", "ceph client id")

var adminSSHKey = ""

// default password set to "ciao"
var adminPassword = "$6$rounds=4096$w9I3hR4g/hu$AnYjaC2DfznbPSG3vxsgtgAS4mJwWBkcR74Y/KHNB5OsfAlA4gpU5j6CHWMOkkt9j.9d7OYJXJ4icXHzKXTAO."

func init() {
	flag.Parse()

	logDirFlag := flag.Lookup("log_dir")
	if logDirFlag == nil {
		glog.Errorf("log_dir does not exist")
		return
	}

	if logDirFlag.Value.String() == "" {
		logDirFlag.Value.Set(logDir)
	}

	if err := os.MkdirAll(logDirFlag.Value.String(), 0755); err != nil {
		glog.Errorf("Unable to create log directory (%s) %v", logDir, err)
		return
	}
}

func main() {
	var wg sync.WaitGroup
	var err error

	ctl := new(controller)
	ctl.tenantReadiness = make(map[string]*tenantConfirmMemo)
	ctl.ds = new(datastore.Datastore)
	ctl.qs = new(quotas.Quotas)

	dsConfig := datastore.Config{
		PersistentURI:     "file:" + *persistentDatastoreLocation,
		TransientURI:      "file:transient?mode=memory&cache=shared",
		InitWorkloadsPath: *workloadsPath,
	}

	err = ctl.ds.Init(dsConfig)
	if err != nil {
		glog.Fatalf("unable to Init datastore: %s", err)
		return
	}

	ctl.qs.Init()
	populateQuotasFromDatastore(ctl.qs, ctl.ds)

	config := &ssntp.Config{
		URI:    *serverURL,
		CAcert: *caCert,
		Cert:   *cert,
		Log:    ssntp.Log,
	}

	ctl.client, err = newSSNTPClient(ctl, config)
	if err != nil {
		// spawn some retry routine?
		glog.Fatalf("unable to connect to SSNTP server")
		return
	}

	ssntpClient := ctl.client.ssntpClient()
	clusterConfig, err := ssntpClient.ClusterConfiguration()
	if err != nil {
		glog.Fatalf("Unable to retrieve Cluster Configuration: %v", err)
		return
	}

	volumeAPIPort = clusterConfig.Configure.Controller.VolumePort
	computeAPIPort = clusterConfig.Configure.Controller.ComputePort
	controllerAPIPort = clusterConfig.Configure.Controller.CiaoPort
	httpsCAcert = clusterConfig.Configure.Controller.HTTPSCACert
	httpsKey = clusterConfig.Configure.Controller.HTTPSKey
	identityURL = clusterConfig.Configure.IdentityService.URL
	serviceUser = clusterConfig.Configure.Controller.IdentityUser
	servicePassword = clusterConfig.Configure.Controller.IdentityPassword
	if *cephID == "" {
		*cephID = clusterConfig.Configure.Storage.CephID
	}

	cnciVCPUs := clusterConfig.Configure.Controller.CNCIVcpus
	cnciMem := clusterConfig.Configure.Controller.CNCIMem
	cnciDisk := clusterConfig.Configure.Controller.CNCIDisk

	adminSSHKey = clusterConfig.Configure.Controller.AdminSSHKey

	if clusterConfig.Configure.Controller.AdminPassword != "" {
		adminPassword = clusterConfig.Configure.Controller.AdminPassword
	}

	ctl.ds.GenerateCNCIWorkload(cnciVCPUs, cnciMem, cnciDisk, adminSSHKey, adminPassword)

	database.Logger = gloginterface.CiaoGlogLogger{}

	logger := gloginterface.CiaoGlogLogger{}
	osprepare.Bootstrap(context.TODO(), logger)
	osprepare.InstallDeps(context.TODO(), controllerDeps, logger)

	idConfig := identityConfig{
		endpoint:        identityURL,
		serviceUserName: serviceUser,
		servicePassword: servicePassword,
	}

	ctl.BlockDriver = func() storage.BlockDriver {
		driver := storage.CephDriver{
			ID: *cephID,
		}
		return driver
	}()

	ctl.id, err = newIdentityClient(idConfig)
	if err != nil {
		glog.Fatal("Unable to authenticate to Keystone: ", err)
		return
	}

	server, err := ctl.createComputeServer()
	if err != nil {
		glog.Fatalf("Error creating compute server: %v", err)
	}
	ctl.httpServers = append(ctl.httpServers, server)

	server, err = ctl.createVolumeServer()
	if err != nil {
		glog.Fatalf("Error creating volume server: %v", err)
	}
	ctl.httpServers = append(ctl.httpServers, server)

	server, err = ctl.createImageServer()
	if err != nil {
		glog.Fatalf("Error creating image server: %v", err)
	}
	ctl.httpServers = append(ctl.httpServers, server)

	host := clusterConfig.Configure.Controller.ControllerFQDN
	if host == "" {
		host, _ = os.Hostname()
	}
	ctl.apiURL = fmt.Sprintf("https://%s:%d", host, controllerAPIPort)

	server, err = ctl.createCiaoServer()
	if err != nil {
		glog.Fatalf("Error creating ciao server: %v", err)
	}
	ctl.httpServers = append(ctl.httpServers, server)

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-signalCh
		glog.Warningf("Received signal: %s", s)
		ctl.ShutdownHTTPServers()
	}()

	for _, server := range ctl.httpServers {
		wg.Add(1)
		go func(server *http.Server) {
			if err := server.ListenAndServeTLS(httpsCAcert, httpsKey); err != http.ErrServerClosed {
				glog.Errorf("Error from HTTP server: %v", err)
			}
			wg.Done()
		}(server)
	}

	wg.Wait()
	glog.Warning("Controller shutdown initiated")
	ctl.qs.Shutdown()
	ctl.ds.Exit()
	ctl.is.ds.Shutdown()
	ctl.client.Disconnect()
}

func (c *controller) createCiaoServer() (*http.Server, error) {
	config := api.Config{URL: c.apiURL, CiaoService: c}

	r := api.Routes(config)
	if r == nil {
		return nil, errors.New("Unable to start Ciao API Service")
	}

	// wrap each route in keystone validation.
	validServices := []osIdentity.ValidService{
		{ServiceType: "compute", ServiceName: "ciao"},
		{ServiceType: "compute", ServiceName: "nova"},
	}

	validAdmins := []osIdentity.ValidAdmin{
		{Project: "service", Role: "admin"},
		{Project: "admin", Role: "admin"},
	}

	err := r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		h := osIdentity.Handler{
			Client:        c.id.scV3,
			Next:          route.GetHandler(),
			ValidServices: validServices,
			ValidAdmins:   validAdmins,
		}

		route.Handler(h)

		return nil
	})

	if err != nil {
		return nil, err
	}

	service := fmt.Sprintf(":%d", controllerAPIPort)

	server := &http.Server{
		Handler: r,
		Addr:    service,
	}

	return server, nil
}

func (c *controller) ShutdownHTTPServers() {
	glog.Warning("Shutting down HTTP servers")
	var wg sync.WaitGroup
	for _, server := range c.httpServers {
		wg.Add(1)
		go func(server *http.Server) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			err := server.Shutdown(ctx)
			if err != nil {
				glog.Errorf("Error during HTTP server shutdown")
			}
			wg.Done()
		}(server)
	}
	wg.Wait()
}
