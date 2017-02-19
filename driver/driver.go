package driver

// Driver represents a cloud storage platform
type Driver interface {
	// Create makes a new volume
	Create(name string, opts map[string]string) (*Volume, error)
	// Remove delets a volume
	Remove(id string) error
	// List gets all volumes
	List() ([]*Volume, error)
	// Get gets a single volume
	Get(id string) (*Volume, error)
	// Mount makes a volume available locally
	Mount(id string) (string, error)
	// Unmount makes a volume unavailable locally
	Unmount(id string) error
}
