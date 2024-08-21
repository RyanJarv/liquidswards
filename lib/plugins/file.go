package plugins

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/types"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/alitto/pond"
	"os"
	"strings"
	"sync"
)

var file = flag.String("file", "", "A file containing a list of additional file to enumerate.")

type NewFilePluginInput struct {
	types.GlobalPluginArgs
}

func NewFile(_ utils.Context, in types.GlobalPluginArgs) types.Plugin {
	return &FilePlugin{
		GlobalPluginArgs: in,
		m:                &sync.RWMutex{},
		covered:          map[string]bool{},
	}
}

func (a *FilePlugin) Name() string {
	return "file"
}

func (a *FilePlugin) Enabled() (bool, string) {
	if *file == "" {
		return false, "no -file arg provided"
	} else {
		return true, fmt.Sprintf("will read from %s", *file)
	}
}

type FilePlugin struct {
	types.GlobalPluginArgs
	FileLocation string
	Pool         *pond.WorkerPool
	m            *sync.RWMutex
	covered      map[string]bool
}

func (f *FilePlugin) Run(ctx utils.Context) {
	file, err := os.ReadFile(f.FileLocation)
	if err != nil {
		ctx.Error.Printf("error reading %s: %s", f.FileLocation, err)
	}

	file = bytes.Trim(file, " \t\n")

	for _, line := range strings.Split(string(file), "\n") {
		arn := strings.Trim(line, " \t")
		if f.Scope != nil && !utils.ArnInScope(f.Scope, arn) {
			continue
		}

		if f.FoundRoles.Add(types.NewRole(arn)) {
			fmt.Printf("File: Found role %s\n", arn)
		}
	}
}
