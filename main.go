package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
)

var (
	region       string
	maxPerSecond int
	profilesStr  string
	save         bool
	load         bool
	path         string
	force        bool
	debug        bool
	l            = utils.L{
		Info:  log.New(os.Stdout, utils.Green.Color("[INFO] "), 0),
		Debug: log.New(os.Stderr, utils.Gray.Color("[DEBUG] "), 0),
		Error: log.New(os.Stderr, utils.Red.Color("[ERROR] "), 0),
	}
)

func main() {
	flag.StringVar(&region, "us-east-1", "", "The AWS Region to use")
	flag.IntVar(&maxPerSecond, "max-per-second", 0, "Maximum requests to send per second.")
	flag.StringVar(&profilesStr, "profiles", "", "List of AWS profiles (seperated by commas) to use for discovering roles.")
	flag.BoolVar(&save, "save", false, "Save list of roles to file specified by path, do not attempt to assume them.")
	flag.BoolVar(&load, "load", false, "Load list of roles to file specified by path then attempt to assume them.")
	flag.BoolVar(&force, "force", false, "Overwrite file specified by -path if it exists.")
	flag.StringVar(&path, "path", "", "Path to use for storing the role list.")
	flag.BoolVar(&debug, "debug", false, "Enable debug output")
	flag.Parse()

	if !debug {
		null, err := os.Open(os.DevNull)
		if err != nil {
			l.Error.Fatalln(err)
		}
		l.Debug.SetOutput(null)
	}

	if region != "" {
		l.Debug.Printf("using region %s\n", region)
	}

	if len(flag.Args()) > 0 {
		l.Error.Fatalln("extra arguments detected, did you mean to pass a comma seperated list to -profiles instead?")
	}

	if (!save && !load) || (save && load) {
		l.Error.Fatalln("must specify either (but not both) -load or -save.")
	}

	if path == "" {
		l.Error.Fatalln("must specify a location for role arns with -path.")
	}

	ctx := context.Background()
	cfgs, err := parseProfiles(ctx, profilesStr, maxPerSecond)
	if err != nil {
		log.Fatalln(err)
	}

	if save {
		if !force {
			if _, err := os.Stat(path); os.IsExist(err) {
				l.Error.Fatalf("the file %s already exists, use -force to overwrite it.\n", path)
			}
		}
		err := Save(ctx, cfgs, path)
		if err != nil {
			l.Error.Fatalln(err)
		}
	} else if load {
		roles, err := Load(path)
		if err != nil {
			l.Error.Fatalln(err)
		}

		assume := AssumeAllRoles(ctx, roles)
		for _, cfg := range cfgs {
			err := assume.Run(cfg)
			if err != nil {
				l.Error.Fatalln(err)
			}
		}

		assume.PrintGraph(cfgs)

	} else {
		l.Error.Fatalln("something went wrong")
	}

}

func parseProfiles(ctx context.Context, profiles string, second int) ([]*aws.Config, error) {
	limiter := utils.NewSessionLimiter(second)

	var ret []*aws.Config
	for _, p := range strings.Split(profiles, ",") {
		p = strings.Trim(p, " \t\n")
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(p))
		if err != nil {
			return nil, fmt.Errorf("failed loading profile %s using regin %s: %w", p, region, err)
		}

		limiter.Instrument(&cfg)

		ret = append(ret, &cfg)
	}
	return ret, nil
}

func AssumeAllRoles(ctx context.Context, roles []Role) *AssumeRoles {
	return &AssumeRoles{
		graph: graph.NewDirectedGraph[string](),
		ctx:   ctx,
		roles: roles,
		wg:    &sync.WaitGroup{},
	}
}

type AssumeRoles struct {
	graph *graph.Graph[string]
	ctx   context.Context
	roles []Role
	wg    *sync.WaitGroup
}

func (a *AssumeRoles) Run(cfg *aws.Config) error {
	resp, err := sts.NewFromConfig(*cfg).GetCallerIdentity(a.ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("calling sts get-caller-identity failed: %w", err)
	}

	a.wg.Add(1)
	a.assumeRoles(cfg, []string{utils.CleanArn(*resp.Arn)})
	a.wg.Wait()
	return nil
}

func (a *AssumeRoles) assumeRoles(cfg *aws.Config, identity []string) {
	l.Debug.Printf("running assumeRoles on %s", strings.Join(identity, " -> "))
	currArn := identity[len(identity)-1]
	if len(identity) == 60 {
		l.Info.Printf("max depth of 50 reached when enumerating %s\n", currArn)
		return
	}

	svc := sts.NewFromConfig(*cfg)
	for _, role := range a.roles {
		externalIds := []*string{}
		switch role.ExternalId.(type) {
		case string:
			externalIds = append(externalIds, aws.String(role.ExternalId.(string)))
		case []string:
			for _, id := range role.ExternalId.([]string) {
				externalIds = append(externalIds, &id)
			}
		case nil:
			externalIds = append(externalIds, nil)
		}

		var externalId *string
		var err error
		for _, extId := range externalIds {
			_, err = svc.AssumeRole(a.ctx, &sts.AssumeRoleInput{
				RoleArn:         aws.String(role.Arn),
				RoleSessionName: aws.String("rhino-assumerole-mapping"),
				DurationSeconds: aws.Int32(900),
				ExternalId:      extId,
			})
			if err != nil {
				externalId = extId
				break
			}
		}

		if err != nil {
			l.Debug.Println(err.Error())
			continue
		}
		newNode := a.graph.AddNode(role.Arn)

		a.graph.AddEdge(currArn, role.Arn, aws.Bool(true), nil)
		arrow := utils.Cyan.Color(" --assumes--> ")
		l.Info.Printf("%s"+arrow+"%s", strings.Join(identity, arrow), role.Arn)

		if utils.VisitedRole(identity[:len(identity)-1], role.Arn) {
			continue
		}

		newCfg := *cfg
		newCfg.Credentials = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(*cfg), role.Arn, func(opts *stscreds.AssumeRoleOptions) {
			opts.ExternalID = externalId
		})

		if newNode {
			a.wg.Add(1)
			go a.assumeRoles(&newCfg, append(identity, role.Arn))
		}
	}
	a.wg.Done()
}

func (a *AssumeRoles) PrintGraph(cfgs []*aws.Config) error {
	fmt.Println(utils.Green.Color("\nAccessed:"))
	for _, cfg := range cfgs {
		resp, err := sts.NewFromConfig(*cfg).GetCallerIdentity(a.ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return err
		}

		start, err := a.graph.GetNode(utils.CleanArn(*resp.Arn))
		if err != nil {
			return err
		}

		graph.DFS[string](a.graph, start, func(e *graph.Edge[string]) bool {
			return e.Accessed != nil && *e.Accessed
		}, nil, []string{}, func(node string, path []string) {
			fmt.Printf("\n")
			for i := 0; i < len(path); i++ {
				fmt.Printf("\t")
			}
			if len(path) == 0 {
				fmt.Printf(" "+utils.Cyan.Color("*")+" %s", node)
			} else {
				fmt.Printf(utils.Cyan.Color("->")+" %s", node)
			}
		})
	}
	fmt.Printf("\n")
	return nil
}

func Load(path string) ([]Role, error) {
	text, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	text = bytes.Trim(text, "\n")

	roles := []Role{}
	for _, line := range strings.Split(string(text), "\n") {
		var role Role
		err := json.Unmarshal([]byte(line), &role)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

// AssumeRolePolicyDocument
//
// 	Example Json:
//			  {
//                "Version": "2012-10-17",
//                "Statement": [
//                    {
//                        "Effect": "Allow",
//                        "Principal": {
//                            "Service": "ssm.amazonaws.com"
//                        },
//                        "Action": "sts:AssumeRole"
//						  "Condition": {
//							  "StringEquals": {
//								  "sts:ExternalId":"asdf"
//						      }
//					      }
//                    }
//                ]
//            },
//
type AssumeRolePolicyDocument struct {
	Version   string `json:"Version"`
	Statement []struct {
		Effect    string `json:"Effect"`
		Principal struct {
			Service   interface{} `json:"Service"`
			Aws       interface{} `json:"Aws"`
			Federated interface{} `json:"Federated"`
		} `json:"Principal"`
		Condition struct {
			StringEquals struct {
				ExternalId interface{} `json:"sts:ExternalId,omitempty"`
			} `json:"StringEquals"`
		} `json:"Condition"`
	} `json:"Statement"`
}

type Role struct {
	Arn        string
	ExternalId interface{}
}

func filterMap(role types.Role) interface{} {
	policyDoc := AssumeRolePolicyDocument{}
	policyStr, err := url.QueryUnescape(*role.AssumeRolePolicyDocument)
	if err != nil {
		l.Error.Printf("could not unescape trust policy: %s\n", err)
		return Role{Arn: *role.Arn}
	}
	err = json.Unmarshal([]byte(policyStr), &policyDoc)
	if err != nil {
		l.Error.Printf("failed to unmarshal role %s\n", *role.Arn)
		l.Debug.Printf("role trust policy: %s\n", policyStr)
		return Role{Arn: *role.Arn}
	}

	for _, s := range policyDoc.Statement {
		if s.Principal.Aws != nil || s.Principal.Federated != nil {
			return Role{Arn: *role.Arn, ExternalId: s.Condition.StringEquals.ExternalId}
		}
	}
	l.Debug.Printf("skipping role %s because it does not match the filter function\n", *role.Arn)
	l.Debug.Printf("trust policy is %s\n", policyStr)
	return nil
}

func Save(ctx context.Context, cfgs []*aws.Config, path string) error {
	file, err := os.Create(path)
	defer file.Close()
	if err != nil {
		return fmt.Errorf("failed to create file at %s", path)
	}
	for _, cfg := range cfgs {
		err := WriteRolesToFile(ctx, cfg, file, filterMap)
		if err != nil {
			return fmt.Errorf("failed writing to %s: %w", path, err)
		}
	}
	return nil
}

func WriteRolesToFile(ctx context.Context, cfg *aws.Config, out io.Writer, filterMap func(types.Role) interface{}) error {
	svc := iam.NewFromConfig(*cfg)

	resp := &iam.ListRolesOutput{IsTruncated: true}

	var err error
	for err == nil && resp.IsTruncated {
		resp, err = svc.ListRoles(ctx, &iam.ListRolesInput{Marker: resp.Marker})
		if err != nil {
			return fmt.Errorf("failed listing roles: %w", err)
		}
		for _, role := range resp.Roles {
			result := filterMap(role)
			if result == nil {
				continue
			}
			text, err := json.Marshal(result)
			if err != nil {
				return err
			}

			_, err = io.WriteString(out, fmt.Sprintln(string(text)))
			if err != nil {
				return fmt.Errorf("failed writing role arn to output: %w", err)
			}
		}
	}
	return nil
}
