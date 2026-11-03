package plugin

import (
	"context"
	"fmt"

	"clubmate/internal/rpc"
)

const (
	onboardingKey     = "onboarding"
	onboardingPending = "pending"
	onboardingDone    = "done"
)

func (c *Control) LoadOnboarding(ctx context.Context, fresh bool) error {
	var state string
	found, err := c.get(ctx, prefsNamespace, onboardingKey, &state)
	if err != nil {
		return fmt.Errorf("load onboarding: %w", err)
	}
	if !found {
		state = onboardingDone
		if fresh && !c.enabled[vscodePlugin] {
			state = onboardingPending
		}
		if err := c.set(ctx, prefsNamespace, onboardingKey, state); err != nil {
			return err
		}
	}
	c.onboarding.Store(state == onboardingPending)
	return nil
}

func (c *Control) Onboarding() bool {
	return c.onboarding.Load()
}

func (c *Control) FinishOnboarding(ctx context.Context, editor string) error {
	switch editor {
	case nvimPlugin:
	case vscodePlugin:
		if err := c.SetEnabled(ctx, vscodePlugin, true); err != nil {
			return err
		}
		if err := c.SetDefault(ctx, editorCapability, vscodePlugin); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: editor must be %s or %s", rpc.ErrInvalidParams, nvimPlugin, vscodePlugin)
	}
	if err := c.set(ctx, prefsNamespace, onboardingKey, onboardingDone); err != nil {
		return err
	}
	c.onboarding.Store(false)
	return nil
}
