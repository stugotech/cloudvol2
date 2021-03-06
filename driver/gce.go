package driver

import (
	"fmt"
	"time"

	"os"

	"path"

	"strconv"

	"errors"

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
	defaultVolumeSizeGb   = 10
)

type gceDriver struct {
	fs          fs.Filesystem
	client      *compute.Service
	project     string
	zone        string
	instance    string
	instanceURI string
	mountPath   string
	diskTypes   map[string]*compute.DiskType
}

type gceVolume struct {
	Volume
	diskURI    string
	devicePath string
}

type gceVolumeOptions struct {
	sizeGb      int64
	diskTypeURI string
}

// NewGceDriver creates a new instance of the GCE volume driver
func NewGceDriver(mountPath string, fs fs.Filesystem) (Driver, error) {
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

	instanceData, err := computeService.Instances.Get(project, zone, instance).Do()
	if err != nil {
		return nil, fmt.Errorf("GCE: error retrieving instance data: %v", err)
	}

	provider := &gceDriver{
		fs:          fs,
		client:      computeService,
		instance:    instance,
		zone:        zone,
		project:     project,
		instanceURI: instanceData.SelfLink,
		mountPath:   mountPath,
	}

	return provider, nil
}

// Create makes a new volume
func (d *gceDriver) Create(id string, optsMap map[string]string) (*Volume, error) {
	// parse options
	opts, err := d.parseVolumeOptions(optsMap)
	if err != nil {
		return nil, err
	}

	// create disk
	vol, err := d.createDisk(id, opts)
	if err != nil {
		return nil, err
	}

	// attach
	if err = d.attachDisk(vol); err != nil {
		return nil, err
	}

	// format
	if err = d.fs.Format(vol.devicePath); err != nil {
		return nil, fmt.Errorf("GCE: error formatting new volume '%s': %v", id, err)
	}

	// mount
	if err = d.mountDisk(vol); err != nil {
		return nil, err
	}

	return &vol.Volume, err
}

// Remove deletes a disk
func (d *gceDriver) Remove(id string) error {
	return fmt.Errorf("GCE: Remove not supported")
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
	if err != nil {
		return nil, err
	}
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
		if err = d.attachDisk(vol); err != nil {
			return "", err
		}
	}

	// mount
	if err = d.mountDisk(vol); err != nil {
		return "", err
	}
	return vol.Path, nil
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
	if err = d.unmountDisk(vol); err != nil {
		return err
	}

	// detach
	if err = d.detachDisk(vol); err != nil {
		return err
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

	log.WithFields(log.Fields{
		"disk":  disk.Name,
		"users": disk.Users,
	}).Info("GCE: found disk")

	if stringInSlice(disk.Users, d.instanceURI) {
		// this disk is already attached
		vol.Ready = true
		log.WithFields(log.Fields{"disk": disk.Name}).Info("disk is attached to current instance")

		attachment, err := d.getAttachedDisk(d.instance, disk.SelfLink)
		if err != nil {
			return nil, fmt.Errorf("GCE: unable to get mount info for disk '%s': %v", id, err)
		}

		vol.devicePath = fmt.Sprintf(devicePathFormat, attachment.DeviceName)

		vol.Path, err = mountpath.GetMountPath(vol.devicePath)
		if err != nil {
			return nil, fmt.Errorf("GCE: unable to get mount info for disk '%s': %v", id, err)
		}

		log.WithFields(log.Fields{
			"disk":       disk.Name,
			"devicePath": vol.devicePath,
			"mount":      vol.Path,
		}).Info("GCE: found volume attachment")
	} else {
		log.WithFields(log.Fields{"disk": disk.Name}).Info("disk not attached to current instance")
	}

	return vol, nil
}

// parseVolumeOptions parses the string options
func (d *gceDriver) parseVolumeOptions(opts map[string]string) (*gceVolumeOptions, error) {
	parsed := &gceVolumeOptions{
		sizeGb: defaultVolumeSizeGb,
	}

	for key, value := range opts {
		if err := d.parseVolumeOption(parsed, key, value); err != nil {
			return nil, fmt.Errorf("GCE: error processing option '%s' with value '%s': %v", key, value, err)
		}
	}

	return parsed, nil
}

// parseVolumeOption parses a single option
func (d *gceDriver) parseVolumeOption(opts *gceVolumeOptions, key string, value string) error {
	var err error
	switch key {
	case "sizeGb":
		if sizeGb, err := strconv.ParseInt(value, 10, 64); err == nil {
			opts.sizeGb = sizeGb
		}
	case "type":
		if diskType, err := d.getDiskType(value); err == nil {
			opts.diskTypeURI = diskType.SelfLink
		}
	default:
		return errors.New("unknown option")
	}
	return err
}

// createDisk creates a new disk
func (d *gceDriver) createDisk(id string, opts *gceVolumeOptions) (*gceVolume, error) {
	disk := &compute.Disk{
		Name:   id,
		SizeGb: opts.sizeGb,
		Type:   opts.diskTypeURI,
	}

	op, err := d.client.Disks.Insert(d.project, d.zone, disk).Do()
	if err != nil {
		return nil, fmt.Errorf("GCE: error creating disk '%s': %v", id, err)
	}

	err = d.waitForOp(op)
	if err != nil {
		return nil, fmt.Errorf("GCE: error creating disk '%s': %v", id, err)
	}

	vol := &gceVolume{
		Volume: Volume{
			Name: id,
		},
		diskURI: op.TargetLink,
	}

	return vol, nil
}

// attachDisk attaches a disk to the current instance
func (d *gceDriver) attachDisk(vol *gceVolume) error {
	attachment := &compute.AttachedDisk{
		DeviceName: vol.Name,
		Source:     vol.diskURI,
	}
	devicePath := fmt.Sprintf(devicePathFormat, vol.Name)

	op, err := d.client.Instances.AttachDisk(d.project, d.zone, d.instance, attachment).Do()
	if err != nil {
		return fmt.Errorf("GCE: error attaching volume '%s'", vol.Name)
	}
	err = d.waitForOp(op)
	if err != nil {
		return fmt.Errorf("GCE: error attaching volume '%s'", vol.Name)
	}

	// set this only on success
	vol.devicePath = devicePath
	vol.Ready = true
	return nil
}

// detachDisk detaches a disk from the current instance
func (d *gceDriver) detachDisk(vol *gceVolume) error {
	op, err := d.client.Instances.DetachDisk(d.project, d.zone, d.instance, vol.Name).Do()
	if err != nil {
		return fmt.Errorf("GCE: error detaching volume '%s': %v", vol.Name, err)
	}
	err = d.waitForOp(op)
	if err != nil {
		return fmt.Errorf("GCE: error detatching volume '%s': %v", vol.Name, err)
	}
	vol.devicePath = ""
	return nil
}

// mountDisk mounts a disk device on the current instance
func (d *gceDriver) mountDisk(vol *gceVolume) error {
	mountPoint := path.Join(d.mountPath, vol.Name)

	if err := d.fs.CreateDir(mountPoint, true, 700); err != nil {
		return fmt.Errorf("GCE: error creating mount point '%s' for volume '%s': %v", mountPoint, vol.Name, err)
	}
	if err := d.fs.Mount(vol.devicePath, mountPoint); err != nil {
		return fmt.Errorf("GCE: error mounting volume '%s' on '%s': %v", vol.Name, mountPoint, err)
	}
	vol.Path = mountPoint
	return nil
}

// unmountDisk removes a disk from the file system
func (d *gceDriver) unmountDisk(vol *gceVolume) error {
	if err := d.fs.Unmount(vol.Path); err != nil {
		return fmt.Errorf("GCE: error unmounting volume '%s' from '%s': %v", vol.Name, vol.Path, err)
	}

	if err := d.fs.RemoveDir(vol.Path, true); err != nil {
		log.WithFields(log.Fields{
			"name":  vol.Name,
			"mount": vol.Path,
			"err":   err,
		}).Warn("GCE: error removing mountpoint")
	}

	vol.Path = ""
	return nil
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

// getDiskType tries to get a disk type by name from the cache and refreshes the cache if not found
func (d *gceDriver) getDiskType(name string) (*compute.DiskType, error) {
	fresh := false

	for !fresh {
		if d.diskTypes == nil {
			if err := d.loadDiskTypes(); err != nil {
				return nil, err
			}
			fresh = true
		}
		if disk, exists := d.diskTypes[name]; exists {
			return disk, nil
		}
		// invalidate cache
		d.diskTypes = nil
	}
	return nil, fmt.Errorf("disk type '%s' not found", name)
}

// loadDiskTypes caches the disk type for the current zone
func (d *gceDriver) loadDiskTypes() error {
	call := d.client.DiskTypes.List(d.project, d.zone)
	d.diskTypes = make(map[string]*compute.DiskType)

	err := call.Pages(context.Background(), func(page *compute.DiskTypeList) error {
		for _, disk := range page.Items {
			d.diskTypes[disk.Name] = disk
		}
		return nil
	})

	return err
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
