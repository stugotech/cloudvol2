package driver

// Driver represents a cloud storage platform
type Driver interface {
	// List gets all volumes
	List() ([]*Volume, error)
	// Get gets a single volume
	Get(id string) (*Volume, error)
	// Mount makes a volume available locally
	Mount(id string) (string, error)
	// Unmount makes a volume unavailable locally
	Unmount(id string) error
}
