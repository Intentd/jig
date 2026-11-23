package expect

import (
	"errors"
	"github.com/intentd/tools/assert"

	"github.com/intentd/kel/core"
	"github.com/intentd/tools/fileio"
	"github.com/intentd/tools/structured"
	"github.com/intentd/tools/term"
)

var (
	readLocal         = fileio.ReadLocal
	strictUnmarshal   = structured.StrictUnmarshal
	stringSliceEqual  = structured.StringSliceEqual
	successMark       = term.SuccessMark
	failureMark       = term.FailureMark
	parseTimeDuration = core.ParseTimeDuration
	applyExtract      = assert.ApplyExtract
)

// errMaxFailures is returned when the run reached --max-failures or a step
// declared onFailure.stop. The remaining expectations do not run; the result
// collected so far is still reported.
var errMaxFailures = errors.New("stopping after failure")
