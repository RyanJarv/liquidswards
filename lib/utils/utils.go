package utils

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go/middleware"
	"go.uber.org/ratelimit"
	"log"
	"strings"
)

type Color string

const (
	Red    Color = "\033[31m"
	Green  Color = "\033[32m"
	Yellow Color = "\033[33m"
	Blue   Color = "\033[34m"
	Purple Color = "\033[35m"
	Cyan   Color = "\033[36m"
	Gray   Color = "\033[37m"
	White  Color = "\033[97m"
)

func (c Color) Color(s ...string) string {
	return string(c) + strings.Join(s, " ") + "\033[0m"
}

type L struct {
	Info  *log.Logger
	Debug *log.Logger
	Error *log.Logger
}

func Colesce(args ...interface{}) interface{} {
	for _, arg := range args {
		if arg != nil {
			return arg
		}
	}
	return nil
}

func VisitedRole(identity []string, role string) bool {
	for _, id := range identity {
		if role == id {
			return true
		}
	}
	return false
}

func CleanArn(arn string) string {
	if strings.Contains(arn, "assumed-role") {
		parts := strings.Split(arn, "/")
		parts[2] = "role"
		arn = strings.Join(parts[0:len(parts)-1], "/")
	}
	return arn
}

func NewSessionLimiter(perSecond int) *SessionLimiter {
	l := &SessionLimiter{}
	if perSecond != 0 {
		l.rl = ratelimit.New(perSecond)
	}
	return l
}

type SessionLimiter struct {
	rl ratelimit.Limiter
}

func (l *SessionLimiter) Instrument(cfg *aws.Config) {
	if l.rl != nil {
		cfg.APIOptions = append(cfg.APIOptions, func(stack *middleware.Stack) error {
			return stack.Initialize.Add(
				middleware.InitializeMiddlewareFunc("RateLimit", l.limit),
				middleware.Before,
			)
		})
	}
}

func (l *SessionLimiter) limit(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (
	out middleware.InitializeOutput, metadata middleware.Metadata, err error,
) {
	l.rl.Take()
	return next.HandleInitialize(ctx, in)
}
