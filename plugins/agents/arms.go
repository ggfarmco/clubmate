package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
)

const (
	modelAuto    = "auto"
	armByAuto    = "auto"
	armByCaller  = "caller"
	armsKey      = "arms/default"
	stratumBlank = "unknown"
)

var (
	expKinds    = []string{"feature", "fix", "refactor", "docs", "test", "ui-check", "review", "spike"}
	expSizes    = []string{"S", "M", "L"}
	defaultArms = []string{"opus", "sonnet"}

	errExpKind = fmt.Errorf("kind is one of %s", strings.Join(expKinds, ", "))
	errExpSize = fmt.Errorf("size is one of %s", strings.Join(expSizes, ", "))
	errArms    = errors.New("arms are model names other than auto, at least one, no duplicates")
)

type armsArgs struct {
	Arms []string `json:"arms"`
}

type armsResult struct {
	Arms []string `json:"arms"`
}

func pickArm(sessionID string, arms []string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return arms[binary.BigEndian.Uint64(sum[:8])%uint64(len(arms))]
}

func validArms(arms []string) bool {
	if len(arms) == 0 {
		return false
	}
	for i, arm := range arms {
		if arm == "" || arm == modelAuto || strings.TrimSpace(arm) != arm || slices.Contains(arms[:i], arm) {
			return false
		}
	}
	return true
}

func (a *app) arms(ctx context.Context) []string {
	var arms []string
	if found, err := a.host.StorageGet(ctx, armsKey, &arms); err != nil || !found || !validArms(arms) {
		return defaultArms
	}
	return arms
}

func (a *app) armsCommand(ctx context.Context, cmd pluginsdk.CommandRun, args armsArgs) (any, error) {
	if args.Arms == nil {
		return armsResult{Arms: a.arms(ctx)}, nil
	}
	if !a.userWord(cmd) {
		return armsResult{}, errSettingUser
	}
	if !validArms(args.Arms) {
		return armsResult{}, errArms
	}
	return armsResult(args), a.host.StorageSet(ctx, armsKey, args.Arms)
}

func (a *app) startExp(ctx context.Context, sessionID string, args newArgs) (*expTag, string, error) {
	if args.Kind != "" && !slices.Contains(expKinds, args.Kind) {
		return nil, "", fmt.Errorf("%w: %q", errExpKind, args.Kind)
	}
	if args.Size != "" && !slices.Contains(expSizes, args.Size) {
		return nil, "", fmt.Errorf("%w: %q", errExpSize, args.Size)
	}
	tag := parseExp(args.Prompt)
	if tag == nil && args.Kind == "" && args.Size == "" && args.Model == "" {
		return nil, "", nil
	}
	if tag == nil {
		tag = &expTag{}
	}
	tag.Kind, tag.Size = cmp.Or(args.Kind, tag.Kind), cmp.Or(args.Size, tag.Size)
	model := args.Model
	switch model {
	case "":
	case modelAuto:
		model = pickArm(sessionID, a.arms(ctx))
		tag.Arm, tag.ArmBy = model, armByAuto
	default:
		tag.Arm, tag.ArmBy = model, armByCaller
	}
	return tag, model, nil
}
