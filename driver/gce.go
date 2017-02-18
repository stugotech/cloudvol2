package driver

import (
	"fmt"
	"time"

	"os"

	"path"

	"cloud.google.com/go/compute/metadata"
	log "github.com/Sirupsen/logrus"
	"github.com/gordonmleigh/mountpath"
	"github.com/stugotech/cloudvol2/fs"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
)

const (
	devicePathFormat      = "/dev/disk/by-id/google-%s"
	operationWaitTimeout  = 5 * time.Second
	operationPollInterval = 100 * time.Millisecond
)

type gceDriver struct {
	client      *compute.Service
	project     string
	zone        string
	instance    string
	instanceURI string
	mountPath   string
}

type gceVolume struct {
	Volume
	diskURI    string
	devicePath string
}

// NewGceDriver creates a new instance of the GCE volume driver
func NewGceDriver(mountPath string) (Driver, error) {
	if !metadata.OnGCE() {
		log.Warn("GCE: not on GCE or can't contact metadata server")
		return nil, fmt.Errorf("GCE: not on GCE or can't contact metadata server")
	}

	creds := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")

	if creds != "" {
		log.WithFields(log.Fields{"file": creds}).Info("GCE: using credentials from GOOGLE_APPLICATION_CREDENTIALS")
	} else {
		log.Info("GCE: using instance default credentials")
	}

	ctx := context.Background()

	client, err := google.DefaultClient(ctx, compute.ComputeScope)
	if err != nil {
		return nil, fmt.Errorf("GCE: error creating client: %s", err)
	}

	computeService, err := compute.New(client)
	if err != nil {
		return nil, fmt.Errorf("GCE: error creating client: %s", err)
	}

	instance, err := metadata.InstanceName()
	if err != nil {
		return nil, fmt.Errorf("GCE: error retrieving instance name: %s", err)
	}

	zone, err := metadata.Zone()
	if err != nil {
		return nil, fmt.Errorf("GCE: error retrieving zone: %s", err)
	}

	project, err := metadata.ProjectID()
	if err != nil {
		return nil, fmt.Errorf("GCE: error retrieving project ID: %s", err)
	}

	log.WithFields(log.Fields{
		"instance": instance,
		"zone":     zone,
		"project":  project,
	}).Info("GCE: detected instance parameters")

	provider := &gceDriver{
		client:      computeService,
		instance:    instance,
		zone:        zone,
		project:     project,
		instanceURI: fmt.Sprintf("projects/%s/zones/%s/instances/%s", project, zone, instance),
		mountPath:   mountPath,
	}

	return provider, nil
}

// List gets info about disks from GCE
func (d *gceDriver) List() ([]*Volume, error) {
	ctx := context.Background()
	call := d.client.Disks.List(d.project, d.zone)
	var volumes []*Volume

	err := call.Pages(ctx, func(page *compute.DiskList) error {
		for _, disk := range page.Items {
			volumes = append(volumes, &Volume{Name: disk.Name})
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("GCE: error listing disks: %v", err)
	}
	return volumes, nil
}

// Get gets info about a volume
func (d *gceDriver) Get(id string) (*Volume, error) {
	vol, err := d.getVolume(id)
	return &vol.Volume, err
}

// Mount mounts a volume
func (d *gceDriver) Mount(id string) (string, error) {
	vol, err := d.getVolume(id)
	if err != nil {
		return "", err
	}

	if vol.Path != "" {
		return vol.Path, fmt.Errorf("GCE: volume '%s' already mounted on '%s'", id, vol.Path)
	}

	if !vol.Ready {
		// attach
		attachment := &compute.AttachedDisk{
			DeviceName: id,
			Source:     vol.diskURI,
		}
		vol.devicePath = fmt.Sprintf(devicePathFormat, id)

		op, err := d.client.Instances.AttachDisk(d.project, d.zone, d.instance, attachment).Do()
		if err != nil {
			return "", fmt.Errorf("GCE: error attaching volume '%s'", id)
		}
		err = d.waitForOp(op)
		if err != nil {
			return "", fmt.Errorf("GCE: error attaching volume '%s'", id)
		}
	}

	// mount
	mountPoint := path.Join(d.mountPath, vol.Name)

	if err = fs.CreateDir(mountPoint, true, 700); err != nil {
		return "", fmt.Errorf("GCE: error creating mount point '%s' for volume '%s': %v", mountPoint, id, err)
	}
	if err = fs.Mount(vol.devicePath, mountPoint); err != nil {
		return "", fmt.Errorf("GCE: error mounting volume '%s' on '%s': %v", id, mountPoint, err)
	}
	vol.Path = mountPoint
	return mountPoint, nil
}

// Unmount unmounts a volume
func (d *gceDriver) Unmount(id string) error {
	vol, err := d.getVolume(id)
	if err != nil {
		return err
	}

	if vol.Path == "" {
		return fmt.Errorf("GCE: volume '%s' not mounted", id)
	}

	// unmount
	if err = fs.Unmount(vol.Path); err != nil {
		return fmt.Errorf("GCE: error unmounting volume '%s' from '%s': %v", id, vol.Path, err)
	}

	if err = fs.RemoveDir(vol.Path, true); err != nil {
		log.WithFields(log.Fields{
			"name":  vol.Name,
			"mount": vol.Path,
			"err":   err,
		}).Warn("GCE: error removing mountpoint")
	}

	vol.Path = ""

	// detach
	op, err := d.client.Instances.DetachDisk(d.project, d.zone, d.instance, id).Do()
	if err != nil {
		return fmt.Errorf("GCE: error detaching volume '%s': %v", id, err)
	}
	err = d.waitForOp(op)
	if err != nil {
		return fmt.Errorf("GCE: error detatching '%s': %v", id, err)
	}

	return nil
}

// getVolume gets info about a volume
func (d *gceDriver) getVolume(id string) (*gceVolume, error) {
	disk, err := d.client.Disks.Get(d.project, d.zone, id).Do()
	if err != nil {
		return nil, fmt.Errorf("GCE: error getting info about disk '%s': %v", id, err)
	}

	vol := &gceVolume{
		Volume: Volume{
			Name: disk.Name,
		},
		diskURI: disk.SelfLink,
	}

	if stringInSlice(disk.Users, d.instanceURI) {
		// this disk is already attached
		vol.Ready = true
		attachment, err := d.getAttachedDisk(d.instance, disk.SelfLink)
		vol.devicePath = fmt.Sprintf(devicePathFormat, attachment.DeviceName)

		vol.Path, err = mountpath.GetMountPath(vol.devicePath)
		if err != nil {
			return nil, fmt.Errorf("GCE: unable to get mount info for disk '%s': %v", id, err)
		}
	}

	return vol, nil
}

// getAttachedDisk gets the disk attachment info for a disk
func (d *gceDriver) getAttachedDisk(instanceName string, diskURI string) (*compute.AttachedDisk, error) {
	instance, err := d.client.Instances.Get(d.project, d.zone, instanceName).Do()
	if err != nil {
		return nil, err
	}
	for _, attachment := range instance.Disks {
		if attachment.Source == diskURI {
			return attachment, nil
		}
	}
	return nil, nil
}

// waitForOp waits for an operation to complete
func (d *gceDriver) waitForOp(op *compute.Operation) error {
	// poll for operation completion
	for start := time.Now(); time.Since(start) < operationWaitTimeout; time.Sleep(operationPollInterval) {
		log.WithFields(log.Fields{
			"project":   d.project,
			"zone":      d.zone,
			"operation": op.Name,
		}).Info("GCE: wait for operation")

		if op, err := d.client.ZoneOperations.Get(d.project, d.zone, op.Name).Do(); err == nil {
			log.WithFields(log.Fields{
				"project":   d.project,
				"zone":      d.zone,
				"operation": op.Name,
				"status":    op.Status,
			}).Info("GCE: operation status")

			if op.Status == "DONE" {
				return nil
			}
		} else {
			// output warning
			log.WithFields(log.Fields{
				"operation":  op.Name,
				"targetLink": op.TargetLink,
				"error":      err,
			}).Warn("GCE: error while getting operation")
		}
	}

	log.WithFields(log.Fields{
		"operation":  op.Name,
		"targetLink": op.TargetLink,
		"timeout":    operationWaitTimeout,
	}).Warn("GCE: timeout while waiting for operation to complete")

	return fmt.Errorf("GCE: timeout while waiting for operation %s on %s to complete", op.Name, op.TargetLink)
}

func stringInSlice(slice []string, target string) bool {
	for _, candidate := range slice {
		if candidate == target {
			return true
		}
	}
	return false
}
