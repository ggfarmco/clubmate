package main

import "context"

const (
	plainKey  = "plain.enabled"
	plainID   = "plain-language"
	plainHint = "New sessions write and think in ASD-STE100 Simplified Technical English, and Russian in УТР."
	plainRule = "Write and think in ASD-STE100 Simplified Technical English; Russian in УТР (упрощённый технический русский)."
)

func (a *app) plainEnabled(ctx context.Context) bool {
	on := true
	if _, err := a.host.StorageGet(ctx, plainKey, &on); err != nil {
		logError("read the plain language setting", err)
		return true
	}
	return on
}

func (a *app) setPlainEnabled(ctx context.Context, on bool) error {
	if err := a.host.StorageSet(ctx, plainKey, on); err != nil {
		return err
	}
	a.refreshSettings(ctx)
	return nil
}
