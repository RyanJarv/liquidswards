package utils

import (
	"context"
	"fmt"
	"github.com/alitto/pond"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/dlsniper/debugger"
	"github.com/go-test/deep"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var Arrow = fmt.Sprintf(" %s ", Cyan.Color(" --assumes--> "))

const (
	colorScheme = "pastel28"

	Red   Color = "\033[31m"
	Green Color = "\033[32m"
	Cyan  Color = "\033[36m"
	Gray  Color = "\033[37m"

	ErrorLogLevel LogLevel = iota
	InfoLogLevel
	DebugLogLevel
)

type Color string

func (c Color) Color(s ...string) string {
	return string(c) + strings.Join(s, " ") + "\033[0m"
}

type LogLevel int

func NewContext(parentCtx context.Context) Context {
	ctx := Context{
		Context: parentCtx,
		Error:   log.New(os.Stderr, Red.Color("[ERROR] "), 0),
		Info:    log.New(os.Stdout, Green.Color("[INFO] "), 0),
		Debug:   log.Default(),
	}

	null, err := os.Open(os.DevNull)
	if err != nil {
		ctx.Error.Fatalln(err)
	}
	ctx.Debug.SetOutput(null)

	return ctx
}

type Context struct {
	context.Context
	LogLevel LogLevel
	Error    *log.Logger
	Info     *log.Logger
	Debug    *log.Logger
}

func (ctx *Context) SetLoggingLevel(level LogLevel) Context {
	ctx.LogLevel = level
	null, err := os.Open(os.DevNull)
	if err != nil {
		ctx.Error.Fatalln(err)
	}

	if int(level) >= int(ErrorLogLevel) {
		ctx.Error = log.New(os.Stderr, Red.Color("[ERROR] "), 0)
	} else {
		ctx.Error.SetOutput(null)
	}

	if int(level) >= int(InfoLogLevel) {
		ctx.Info = log.New(os.Stdout, Green.Color("[INFO] "), 0)
	} else {
		ctx.Info.SetOutput(null)
	}

	if int(level) >= int(DebugLogLevel) {
		ctx.Debug = log.New(os.Stderr, Gray.Color("[DEBUG] "), 0)
	} else {
		ctx.Info.SetOutput(null)
	}
	return *ctx
}

func (ctx Context) WithCancel() (Context, context.CancelFunc) {
	var cancel context.CancelFunc
	ctx.Context, cancel = context.WithCancel(ctx.Context)
	return Context{
		Context: ctx,
		Info:    ctx.Info,
		Debug:   ctx.Debug,
		Error:   ctx.Error,
	}, cancel
}

func (ctx Context) IsRunning(msg ...string) bool {
	select {
	case <-ctx.Done():
		if len(msg) != 0 {
			ctx.Info.Println(msg)
		}
		return false
	default:
		return true
	}
}

func (ctx Context) IsDone(msg ...string) bool {
	return !ctx.IsRunning(msg...)
}

func (ctx Context) Sleep(delay time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

func ColorFromArn() colorFromArn {
	return colorFromArn{}
}

type colorFromArn []*string

func (c *colorFromArn) Get(arn string) string {
	accountId := strings.Split(arn, ":")[4]
	for i, prev := range *c {
		if *prev == accountId {
			resp := "/" + colorScheme + "/" + strconv.Itoa(i+1)
			return resp
		}
	}
	*c = append(*c, &accountId)
	resp := "/" + colorScheme + "/" + strconv.Itoa(len(*c))
	return resp
}

func In[T comparable](haystack []T, needle T) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

type TimeSlice struct {
	Start time.Time
	End   time.Time
}

func TimeSlices(duration time.Duration, minutes int) []TimeSlice {
	start := time.Now().Add(-duration)
	// Round time to beginning of last hour.
	start = time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), start.Minute(), 0, 0, start.Location())

	var slices []TimeSlice

	period := time.Minute * time.Duration(minutes)
	for i := 0; i < int(duration.Minutes())/minutes; i++ {
		slices = append(slices, TimeSlice{
			Start: start.Add(period * time.Duration(i)),
			End:   start.Add(period*time.Duration(i) + period),
		})
	}

	return slices
}

func GetCallerArn(ctx Context, cfg aws.Config) (string, error) {
	svc := sts.NewFromConfig(cfg)
	resp, err := svc.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("unable to retrieve caller targetArn: %w", err)
	}
	return *resp.Arn, nil
}

func SetDebugLabels(labels ...string) {
	debugger.SetLabels(func() []string {
		return labels
	})
}

func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func MonitorPoolStats(ctx Context, msg string, pool *pond.WorkerPool) {
	go func() {
		for ctx.IsRunning("exiting monitor pool:", msg) {
			ctx.Sleep(time.Second * 5)
			fmt.Println(
				msg,
				"wait:", pool.WaitingTasks(),
				"run:", pool.RunningWorkers(),
				"idle:", pool.IdleWorkers(),
				"done:", pool.CompletedTasks(),
				"fail:", pool.FailedTasks(),
				"maxCap:", pool.MaxCapacity(),
				"maxWork:", pool.MaxWorkers())
		}
	}()
}

func SplitCommas(s string) []string {
	var resp []string
	for _, p := range strings.Split(s, ",") {
		resp = append(resp, strings.Trim(p, " \t\n"))
	}
	return resp
}

func FilterDuplicates[T comparable](slice []T) []T {
	var found map[T]bool
	var resp []T
	for _, v := range slice {
		if _, ok := found[v]; !ok {
			resp = append(resp, v)
		}
	}
	return resp
}

func RemoveDefaults[T comparable](slice []T) []T {
	var resp []T
	for _, value := range slice {
		v := reflect.ValueOf(value)
		if v.Interface() == reflect.Zero(v.Type()).Interface() {
			continue
		}
		resp = append(resp, value)
	}
	return resp
}

func AccountIdFromArn(arn string) (string, error) {
	p := strings.Split(arn, ":")
	if len(p) != 6 {
		return "", fmt.Errorf("does not appear to be a valid targetArn: %s", arn)
	}
	return p[4], nil
}

func ArnInScope(scope []string, arn string) bool {
	accountId := Must(AccountIdFromArn(arn))
	if In(scope, accountId) {
		return true
	}
	return false
}

func ExpandPath(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return path, nil
	}
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	return filepath.Abs(path)
}

func DeepDiff[T any, K any](got T, want K) string {
	typeName := reflect.TypeOf(got).Name()
	if diff := deep.Equal(got, want); diff != nil {
		msg := "got != want:\n"
		for i, line := range diff {
			msg = msg + fmt.Sprintf("\t%d) %s.%s\n", i, typeName, line)
		}
		return msg
	}
	return ""
}

func NewTestCreds(canExpire bool, source string) aws.Credentials {
	return aws.Credentials{
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		SessionToken:    "test",
		CanExpire:       canExpire,
		Expires:         time.Now().Round(time.Hour * 24 * 30),
		Source:          source,
	}
}

func Keys[T comparable, K any](v map[T]K) []T {
	result := []T{}
	for k, _ := range v {
		result = append(result, k)
	}
	return result
}

type StsAssumeRoleAPI interface {
	AssumeRole(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

type AssumeRoleFunc = func(ctx context.Context, in *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
