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

package payloads

// ServiceType is used to define OpenStack service types, like e.g. the image
// or identity ones.
type ServiceType string

// StorageType is used to define the configuration backend storage type.
type StorageType string

const (
	// Glance is used to define the imaging service.
	Glance ServiceType = "glance"

	// Keystone is used to define the identity service.
	Keystone ServiceType = "keystone"
)

const (
	// Filesystem defines the local filesystem backend storage type for the
	// configuration data.
	Filesystem StorageType = "file"

	// Etcd defines the etcd backend storage type for the configuration data.
	Etcd StorageType = "etcd"
)

func (s ServiceType) String() string {
	switch s {
	case Glance:
		return "glance"
	case Keystone:
		return "keystone"
	}

	return ""
}

func (s StorageType) String() string {
	switch s {
	case Filesystem:
		return "file"
	case Etcd:
		return "etcd"
	}

	return ""
}

// ConfigureScheduler contains the unmarshalled configurations for the
// scheduler service.
type ConfigureScheduler struct {
	ConfigStorageType StorageType `yaml:"storage_type"`
	ConfigStorageURI  string      `yaml:"storage_uri"`
}

// ConfigureController contains the unmarshalled configurations for the
// controller service.
type ConfigureController struct {
	ComputePort      int    `yaml:"compute_port"`
	HTTPSCACert      string `yaml:"compute_ca"`
	HTTPSKey         string `yaml:"compute_cert"`
	IdentityUser     string `yaml:"identity_user"`
	IdentityPassword string `yaml:"identity_password"`
}

// ConfigureLauncher contains the unmarshalled configurations for the
// launcher service.
type ConfigureLauncher struct {
	ComputeNetwork    []string `yaml:"compute_net"`
	ManagementNetwork []string `yaml:"mgmt_net"`
	DiskLimit         bool     `yaml:"disk_limit"`
	MemoryLimit       bool     `yaml:"mem_limit"`
}

// ConfigureLauncherLegacy contains the unmarshalled legacy configuration
// for the launcher service. The legacy configuration only takes one compute
// and one management subnet at most.
type ConfigureLauncherLegacy struct {
	ComputeNetwork    string `yaml:"compute_net"`
	ManagementNetwork string `yaml:"mgmt_net"`
	DiskLimit         bool   `yaml:"disk_limit"`
	MemoryLimit       bool   `yaml:"mem_limit"`
}

// ConfigureStorage contains the unmarshalled configurations for the
// Ceph storage driver.
type ConfigureStorage struct {
	SecretPath string `yaml:"secret_path"`
	CephID     string `yaml:"ceph_id"`


}

// ConfigureService contains the unmarshalled configurations for the resources
// of the configurations.
type ConfigureService struct {
	Type ServiceType `yaml:"type"`
	URL  string      `yaml:"url"`
}

// ConfigurePayload is a wrapper to read and unmarshall all possible
// configurations for the following services: scheduler, controller, launcher,
// imaging and identity.
type ConfigurePayload struct {
	Scheduler       ConfigureScheduler  `yaml:"scheduler"`
	Storage         ConfigureStorage    `yaml:"storage"`
	Controller      ConfigureController `yaml:"controller"`
	Launcher        ConfigureLauncher   `yaml:"launcher"`
	ImageService    ConfigureService    `yaml:"image_service"`
	IdentityService ConfigureService    `yaml:"identity_service"`
}

// ConfigurePayloadLegacy is a wrapper to read and unmarshall all possible
// configurations for the following services: scheduler, controller, launcher,
// imaging and identity.
// The legacy part of this structure is on the launcher configuration. See
// ConfigureLauncherLegacy.
type ConfigurePayloadLegacy struct {
	Scheduler       ConfigureScheduler      `yaml:"scheduler"`
	Controller      ConfigureController     `yaml:"controller"`
	Launcher        ConfigureLauncherLegacy `yaml:"launcher"`
	ImageService    ConfigureService        `yaml:"image_service"`
	IdentityService ConfigureService        `yaml:"identity_service"`
}

// Configure represents the SSNTP CONFIGURE command payload.
type Configure struct {
	Configure ConfigurePayload `yaml:"configure"`
}

// ConfigureLegacy represents the SSNTP CONFIGURE command legacy payload.
// See ConfigureLauncherLegacy for an explanation about the difference between
// the current and legacy payloads.
type ConfigureLegacy struct {
	Configure ConfigurePayloadLegacy `yaml:"configure"`
}

// InitDefaults initializes default vaulues for Configure structure.
func (conf *Configure) InitDefaults() {
	conf.Configure.Scheduler.ConfigStorageType = Filesystem
	conf.Configure.Controller.ComputePort = 8774
	conf.Configure.ImageService.Type = Glance
	conf.Configure.IdentityService.Type = Keystone
	conf.Configure.Launcher.DiskLimit = true
	conf.Configure.Launcher.MemoryLimit = true
}

func (conf *Configure) ConvertFromLegacy(legacyConf *ConfigureLegacy) {
	conf.Configure.Scheduler = legacyConf.Configure.Scheduler
	conf.Configure.Controller = legacyConf.Configure.Controller
	conf.Configure.ImageService = legacyConf.Configure.ImageService
	conf.Configure.IdentityService = legacyConf.Configure.IdentityService

	conf.Configure.Launcher.DiskLimit = legacyConf.Configure.Launcher.DiskLimit
	conf.Configure.Launcher.MemoryLimit = legacyConf.Configure.Launcher.MemoryLimit
	if len(legacyConf.Configure.Launcher.ComputeNetwork) != 0 {
		conf.Configure.Launcher.ComputeNetwork =
			append(conf.Configure.Launcher.ComputeNetwork,
				legacyConf.Configure.Launcher.ComputeNetwork)
	}

	if len(legacyConf.Configure.Launcher.ManagementNetwork) != 0 {
		conf.Configure.Launcher.ManagementNetwork =
			append(conf.Configure.Launcher.ManagementNetwork,
				legacyConf.Configure.Launcher.ManagementNetwork)
	}
}
