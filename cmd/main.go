package main

import (
	"fmt"
	"os"

	manifestdata "silo-plugin-aiometadata"
	"silo-plugin-aiometadata/internal/plugin"

	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/hashicorp/go-hclog"
)

func main() {
	logger := hclog.New(&hclog.LoggerOptions{Name: "silo-plugin-aiometadata", Level: hclog.Info, Output: os.Stderr, JSONFormat: true})
	p := plugin.New(logger, nil)
	m, err := manifest.LoadWithChecksum(manifestdata.JSON, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	p.SetManifest(m)
	runtime.Serve(runtime.ServeConfig{Logger: logger, Servers: runtime.CapabilityServers{Runtime: p, MetadataProvider: p}})
}
