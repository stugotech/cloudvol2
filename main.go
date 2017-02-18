package main

import (
	"flag"
	"fmt"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
	"github.com/stugotech/cloudvol2/driver"
	"github.com/stugotech/cloudvol2/plugin"
)

const (
	driverName = "cloudvol"
	mountPath  = "/mnt"
)

func main() {
	log.WithFields(log.Fields{"pid": os.Getpid()}).Info("*** STARTED cloudvol volume driver ***")

	mode := flag.String("mode", "fs", "storage mode (fs, gce, aws)")
	port := flag.Int("port", 8080, "port to listen on (ignored if sock is set)")
	sock := flag.Bool("sock", false, "listen on a unix socket")
	flag.Parse()

	log.WithFields(log.Fields{"mode": *mode}).Info("creating storage driver")

	d, err := createStorageDriver(*mode, mountPath)

	if err != nil {
		log.WithError(err).Fatal("stopping due to last error")
	}

	plugin := plugin.NewCloudvolPlugin(d)
	handler := volume.NewHandler(plugin)

	if !*sock {
		log.WithFields(log.Fields{"port": *port}).Infof("listening on port %d", *port)
		addr := fmt.Sprintf(":%d", *port)
		err = handler.ServeTCP(driverName, addr, nil)
	} else {
		log.Infof("listening on socket file")
		err = handler.ServeUnix(driverName, 0)
	}

	if err != nil {
		log.Fatal(err)
	} else {
		log.Info("Started.")
	}
}

func createStorageDriver(name string, mountPath string) (driver.Driver, error) {
	if name == "gce" {
		return driver.NewGceDriver(mountPath)
	}
	return nil, fmt.Errorf("unknown driver type '%s'", name)
}
