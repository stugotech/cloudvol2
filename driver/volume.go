package driver

// Volume represents a docker volume
type Volume struct {
	Name  string
	Path  string
	Ready bool
}
