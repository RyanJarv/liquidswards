package plugins

import (
	"bytes"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/creds"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/alitto/pond"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"io/ioutil"
	"strings"
	"sync"
)

type NewFilePluginInput struct {
	GlobalPluginArgs
	FileLocation string
}

func NewFilePlugin(in *NewFilePluginInput) *FilePlugin {
	return &FilePlugin{
		NewFilePluginInput: in,
		m:                  &sync.RWMutex{},
		covered:            map[string]bool{},
	}
}

type FilePlugin struct {
	*NewFilePluginInput
	Pool    *pond.WorkerPool
	m       *sync.RWMutex
	covered map[string]bool
}

func (f *FilePlugin) Run(ctx utils.Context, _ *creds.Config) {
	file, err := ioutil.ReadFile(f.FileLocation)
	if err != nil {
		ctx.Error.Printf("error reading %s: %s", f.FileLocation, err)
	}

	file = bytes.Trim(file, " \t\n")

	for _, line := range strings.Split(string(file), "\n") {
		arn := strings.Trim(line, " \t")
		if f.Scope != nil && !utils.ArnInScope(f.Scope, arn) {
			continue
		}

		if f.Lq.AddUnique(arn, types.Role{Arn: aws.String(arn)}) {
			fmt.Printf("File: Found role %s\n", arn)
		}
	}
}
