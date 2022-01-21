package creds

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RyanJarv/liquidswards/lib/graph"
	"github.com/RyanJarv/liquidswards/lib/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/sts/types"
	"strings"
	"sync"
	"time"
)

type State int

const (
	ActiveState State = iota
	RefreshingState
	FailedState
)

type IConfig interface {
	MarshalJSON() ([]byte, error)
	UnmarshalJSON([]byte) error
	ID() string
	Arn() string
	Name() string
	Account() string
	Config() aws.Config
	Assume(ctx utils.Context, arn string) (*Config, error)
	State() State
	Refresh(utils.Context) (*Config, error)
	SetState(State)
	SetGraph(graph interface{})
}

type SourceType int

const (
	SourceProfile SourceType = iota
	SourceAssumeRole
)

type Source struct {
	Type SourceType
	Name string
	Arn  string
}

func NewConfig(ctx utils.Context, creds aws.Credentials, region string, src Source) (*Config, error) {
	arnParts := strings.Split(src.Arn, ":")
	if len(arnParts) != 6 {
		return nil, fmt.Errorf("does not appear to be a valid targetArn: %s", src.Arn)
	}

	c := &Config{
		initalCreds: creds,
		cfg: aws.Config{Region: region, Credentials: credentials.StaticCredentialsProvider{
			Value: aws.Credentials{
				AccessKeyID:     creds.AccessKeyID,
				SecretAccessKey: creds.SecretAccessKey,
				SessionToken:    creds.SessionToken,
				Source:          src.Arn,
			},
		}},
		source:  src,
		ctx:     ctx,
		account: arnParts[4],
	}
	if c.AssumeRole == nil {
		c.AssumeRole = sts.NewFromConfig(c.cfg).AssumeRole
	}
	return c, nil
}

// SetGraph needs to be called with the graph and initial creds before Config is used.
// We can't do this in NewConfig because that is used to serialize/deserialize JSON (and therefor doesn't have access
// to the graph object).
func (c *Config) SetGraph(g interface{}) {
	c.graph = g.(*graph.Graph[*Config])
	c.cfg.Credentials = c.CredProvider(c.initalCreds)
}

type Config struct {
	source Source
	cfg    aws.Config
	ctx    utils.Context
	state  State
	m      sync.Mutex

	// A bit of a hack for mocking, imagine there is a better way to do this.
	AssumeRole  func(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
	graph       *graph.Graph[*Config]
	account     string
	initalCreds aws.Credentials
}

func (c *Config) State() State {
	return c.state
}

func (c *Config) SetState(s State) {
	c.state = s
}

func (c *Config) Config() aws.Config {
	return c.cfg
}

func (c *Config) Assume(ctx utils.Context, arn string) (*Config, error) {
	resp, err := c.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         aws.String(arn),
		RoleSessionName: aws.String("liquidswards"),
		//DurationSeconds: aws.Int32(900), // TODO: Make this configurable
	})
	if err != nil {
		return nil, fmt.Errorf("Assume(): %w", err)
	}

	creds := aws.Credentials{
		AccessKeyID:     *resp.Credentials.AccessKeyId,
		SecretAccessKey: *resp.Credentials.SecretAccessKey,
		SessionToken:    *resp.Credentials.SessionToken,
		Expires:         *resp.Credentials.Expiration,
		CanExpire:       true,
		Source:          c.Arn(),
	}

	src := Source{
		Type: SourceAssumeRole,
		Name: arn,
		Arn:  arn,
	}
	newCfg, err := NewConfig(ctx, creds, c.cfg.Region, src)
	newCfg.SetGraph(c.graph)
	return newCfg, err
}

// ID is used to implement graph.INode which returns the principal ARN.
func (c *Config) ID() string {
	return c.Arn()
}

// Name returns the role/user name without the path.
func (c *Config) Name() string {
	p := strings.Split(c.Arn(), "/")
	return p[len(p)-1]
}

func (c *Config) Arn() string {
	return c.source.Arn
}

func (c *Config) Account() string {
	return c.account
}

func (c *Config) Refresh(ctx utils.Context) (*Config, error) {
	c.state = RefreshingState
	node, err := c.graph.GetNode(c.Arn())
	if err != nil {
		return nil, fmt.Errorf("RefreshingState(): %w", err)
	}

	var cfg *Config
	for _, src := range node.Inbound() {
		cfg, err = src.Value().Assume(ctx, c.Arn())
		if err == nil {
			break
		}
	}

	if cfg == nil {
		c.SetState(FailedState)
		in := node.Inbound()
		names := strings.Join(utils.Keys(in), ", ")
		return nil, fmt.Errorf("unable to refresh credentials from: %s", names)
	} else {
		c.SetState(ActiveState)
	}

	return cfg, nil
}

// CredProvider uses the passed aws.Credentials if valid, otherwise credentials are refreshed from the graph.
func (c *Config) CredProvider(creds aws.Credentials) aws.CredentialsProvider {
	return aws.NewCredentialsCache(
		aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			if !creds.Expired() {
				return creds, nil
			}

			cfg, err := c.Refresh(utils.NewContext(ctx))
			if err != nil {
				return aws.Credentials{}, err
			}

			creds, err := cfg.Config().Credentials.Retrieve(ctx)
			return creds, err
		}),
	)
}

type JsonConfig struct {
	Arn         string
	State       State
	Region      string
	Credentials aws.Credentials
	Source      Source
}

func (c *Config) MarshalJSON() ([]byte, error) {
	var creds aws.Credentials
	var err error

	if c.cfg.Credentials != nil {
		creds, err = c.cfg.Credentials.Retrieve(context.Background())
		if err != nil {
			return nil, fmt.Errorf("Save(): %w", err)
		}
	}

	obj := JsonConfig{
		Arn:         c.Arn(),
		State:       c.state,
		Region:      c.cfg.Region,
		Credentials: creds,
		Source:      c.source,
	}
	r, err := json.Marshal(obj)
	return r, err
}

func (c *Config) UnmarshalJSON(bytes []byte) error {
	// TODO: Implement limiter
	var obj JsonConfig
	err := json.Unmarshal(bytes, &obj)
	if err != nil {
		return fmt.Errorf("unmarshalling: %w", err)
	}

	cfg, err := NewConfig(c.ctx, obj.Credentials, obj.Region, obj.Source)
	if err != nil {
		return fmt.Errorf("UnmarshalJSON(): %w", err)
	}

	*c = *cfg
	return nil
}

func ParseProfiles(ctx utils.Context, profiles string, region string, g *graph.Graph[*Config]) ([]*Config, error) {
	var ret []*Config
	for _, p := range utils.SplitCommas(profiles) {
		awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(p))
		if err != nil {
			return nil, fmt.Errorf("loading profile %s using region %s: %w", p, region, err)
		}

		creds, err := awsCfg.Credentials.Retrieve(ctx)
		if err != nil {
			return ret, fmt.Errorf("ParseProfiles(): %w", err)
		}

		arn, err := utils.GetCallerArn(ctx, awsCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to call sts:GetCallerArn using the %s profile: %w", p, err)
		}

		cfg, err := NewConfig(ctx, creds, region, Source{Type: SourceProfile, Name: p, Arn: arn})
		if err != nil {
			return nil, fmt.Errorf("ParseProfiles(): %w", err)
		}
		cfg.SetGraph(g)

		ret = append(ret, cfg)
	}
	return ret, nil
}

// ParseScope returns the accounts included in the comma delimited scopeStr as well as any accounts passed in cfgs.
func ParseScope(scopeStr string, cfgs []*Config) []string {
	scope := utils.SplitCommas(scopeStr)
	for _, cfg := range cfgs {
		scope = append(scope, cfg.Account())
	}
	return utils.FilterDuplicates(utils.RemoveDefaults(scope))
}

// NewTestAssumesAllConfig is kept here for now to allow using it from other modules.
func NewTestAssumesAllConfig(srcType SourceType, name string, g *graph.Graph[*Config]) (*Config, error) {
	src := Source{
		Type: srcType,
		Name: fmt.Sprintf("arn:aws:iam::123456789012:%s", name),
		Arn:  fmt.Sprintf("arn:aws:iam::123456789012:%s", name),
	}

	ctx := utils.NewContext(context.Background())
	creds := aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test", SessionToken: "test"}
	cfg, err := NewConfig(ctx, creds, "us-east-1", src)
	if err != nil {
		return nil, fmt.Errorf("NewTestAssumesAllConfig(): %w", err)
	}

	cfg.SetGraph(g)

	// TODO: Test SetGraph's credential provider.
	// For now just override it with static creds.
	cfg.cfg.Credentials = credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     "test",
			SecretAccessKey: "test",
			SessionToken:    "test",
			Source:          src.Arn,
		},
	}

	cfg.AssumeRole = func(ctx context.Context, in *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
		return &sts.AssumeRoleOutput{
			AssumedRoleUser: &types.AssumedRoleUser{
				Arn:           aws.String(fmt.Sprintf("%s-arn", *in.RoleArn)),
				AssumedRoleId: aws.String(fmt.Sprintf("%s-id", *in.RoleArn)),
			},
			Credentials: &types.Credentials{
				AccessKeyId:     aws.String("test"),
				SecretAccessKey: aws.String("test"),
				SessionToken:    aws.String("test"),
				Expiration:      aws.Time(time.Now().Round(time.Hour * 24 * 30)),
			},
		}, nil
	}
	return cfg, nil
}
