package plugin

import (
	"fmt"

	"github.com/stugotech/cloudvol2/driver"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

type cloudvolPlugin struct {
	driver driver.Driver
}

// NewCloudvolPlugin creates a new instance of the volume plugin
func NewCloudvolPlugin(driver driver.Driver) volume.Driver {
	return &cloudvolPlugin{
		driver: driver,
	}
}

// Cabailities returns the capabilities of the driver
func (p *cloudvolPlugin) Capabilities(r volume.Request) volume.Response {
	log.Info("REQUEST: Capabilities")
	return volume.Response{Capabilities: volume.Capability{Scope: "global"}}
}

// Create creates a new volume.
func (p *cloudvolPlugin) Create(r volume.Request) volume.Response {
	log.WithFields(log.Fields{"name": r.Name, "opts": r.Options}).Info("REQUEST: Create")

	vol, err := p.driver.Create(r.Name, r.Options)
	if err != nil {
		log.WithFields(log.Fields{"name": r.Name, "err": err}).Error("RESPONSE: Create: error")
		return volume.Response{Err: fmt.Sprintf("error creating volume '%s': %v", r.Name, err)}
	}

	return volume.Response{
		Volume: &volume.Volume{
			Name:       vol.Name,
			Mountpoint: vol.Path,
		},
	}
}

// List lists all volumes the driver knows of.
func (p *cloudvolPlugin) List(r volume.Request) volume.Response {
	log.Info("REQUEST: List")

	driverVols, err := p.driver.List()

	if err != nil {
		log.WithError(err).Error("RESPONSE: List: error")
		return volume.Response{Err: fmt.Sprintf("error listing volumes: %v", err)}
	}

	var vols []*volume.Volume

	for _, vol := range driverVols {
		log.WithFields(log.Fields{
			"name":  vol.Name,
			"mount": vol.Path,
			"ready": vol.Ready,
		}).Info("RESPONSE: List: found volume")
		vols = append(vols, &volume.Volume{Name: vol.Name, Mountpoint: vol.Path})
	}

	return volume.Response{Volumes: vols}
}

// Get gets a specific volume.
func (p *cloudvolPlugin) Get(r volume.Request) volume.Response {
	log.WithFields(log.Fields{"name": r.Name}).Info("REQUEST: Get")

	vol, err := p.driver.Get(r.Name)

	if err != nil {
		log.WithFields(log.Fields{"name": r.Name, "err": err}).Error("RESPONSE: Get: error")
		return volume.Response{Err: fmt.Sprintf("error getting volume '%s': %v", r.Name, err)}
	}

	log.WithFields(log.Fields{
		"name":  vol.Name,
		"mount": vol.Path,
		"ready": vol.Ready,
	}).Info("RESPONSE: Get: found")
	return volume.Response{Volume: &volume.Volume{Name: vol.Name, Mountpoint: vol.Path}}
}

// Remove deletes a specific volume.
func (p *cloudvolPlugin) Remove(r volume.Request) volume.Response {
	log.WithFields(log.Fields{"name": r.Name}).Info("REQUEST: Remove")

	if err := p.driver.Remove(r.Name); err != nil {
		log.WithFields(log.Fields{"name": r.Name, "err": err}).Error("RESPONSE: Remove: error")
		return volume.Response{Err: fmt.Sprintf("error removing volume '%s': %v", r.Name, err)}
	}

	return volume.Response{}
}

// Path gets the path of a given volume.
func (p *cloudvolPlugin) Path(r volume.Request) volume.Response {
	log.WithFields(log.Fields{"name": r.Name}).Info("REQUEST: Path")
	vol, err := p.driver.Get(r.Name)

	if err != nil {
		log.WithFields(log.Fields{"name": r.Name, "err": err}).Error("RESPONSE: Path: error")
		return volume.Response{Err: fmt.Sprintf("error getting volume '%s': %v", r.Name, err)}
	}

	log.WithFields(log.Fields{"name": r.Name, "mount": vol.Path}).Info("RESPONSE: Path")
	return volume.Response{Mountpoint: vol.Path}
}

// Mount mounts a volume onto the local file system.
func (p *cloudvolPlugin) Mount(r volume.MountRequest) volume.Response {
	log.WithFields(log.Fields{"name": r.Name, "id": r.ID}).Info("REQUEST: Mount")
	path, err := p.driver.Mount(r.Name)

	if err != nil {
		log.WithFields(log.Fields{"name": r.Name, "err": err}).Error("RESPONSE: Mount: error mounting")
		return volume.Response{Err: fmt.Sprintf("error mounting volume '%s': %v", r.Name, err)}
	}
	log.WithFields(log.Fields{"name": r.Name, "mount": path}).Info("RESPONSE: Mount: mounted")
	return volume.Response{Mountpoint: path}
}

// Unmount removes a volume from the local file system.
func (p *cloudvolPlugin) Unmount(r volume.UnmountRequest) volume.Response {
	log.WithFields(log.Fields{"name": r.Name, "id": r.ID}).Info("REQUEST: Unmount")

	if err := p.driver.Unmount(r.Name); err != nil {
		log.WithFields(log.Fields{"name": r.Name, "err": err}).Error("RESPONSE: Unmount: error unmounting")
		return volume.Response{Err: fmt.Sprintf("error unmounting volume '%s': %v", r.Name, err)}
	}
	log.WithFields(log.Fields{"name": r.Name}).Info("RESPONSE: Unmount: done")
	return volume.Response{}
}
