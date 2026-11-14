package require

import "time"

type TestingT interface{}

func Eventually(t TestingT, cond func() bool, waitFor, tick time.Duration, msg ...any) {}

func Never(t TestingT, cond func() bool, waitFor, tick time.Duration, msg ...any) {}
