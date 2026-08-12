package resolve

import (
	"fmt"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// resultVar is the convention a dynamic layer must follow: its output is
// the dict assigned to this top-level global (documented in CONCEPTION.md
// section 9's Recipe example).
const resultVar = "res"

// StarlarkOptions bounds a dynamic layer's execution (CONCEPTION.md
// section 5.4): Starlark is hermetic by construction (no file/network/clock
// access unless the host exposes it, which the resolver never does), so
// these limits are the resolver's only remaining defense against a runaway
// script — no method whitelist is needed.
type StarlarkOptions struct {
	MaxSteps uint64
	Timeout  time.Duration
}

func DefaultStarlarkOptions() StarlarkOptions {
	return StarlarkOptions{
		MaxSteps: 1_000_000,
		Timeout:  2 * time.Second,
	}
}

// runDynamicLayer executes a Starlark dynamic layer against the config
// accumulated by previous layers (CONCEPTION.md section 5.3): each
// top-level key of accumulated is exposed as a predeclared global the
// script can read directly by name, and nothing else — no I/O primitive is
// ever registered, so the script has no ambient authority beyond that
// input.
func runDynamicLayer(accumulated map[string]any, script, layerName string, opts StarlarkOptions) (map[string]any, error) {
	predeclared := starlark.StringDict{}
	for k, v := range accumulated {
		sv, err := goToStarlark(v)
		if err != nil {
			return nil, fmt.Errorf("layer %q: exposing %q to starlark: %w", layerName, k, err)
		}
		predeclared[k] = sv
	}

	thread := &starlark.Thread{Name: layerName}
	if opts.MaxSteps > 0 {
		thread.SetMaxExecutionSteps(opts.MaxSteps)
	}
	if opts.Timeout > 0 {
		timer := time.AfterFunc(opts.Timeout, func() {
			thread.Cancel("layer exceeded timeout")
		})
		defer timer.Stop()
	}

	// TopLevelControl allows the `if` seen at module scope in the section 9
	// example; layers are short config-generation snippets, not libraries,
	// so no other file option is enabled.
	fileOpts := &syntax.FileOptions{TopLevelControl: true}
	globals, err := starlark.ExecFileOptions(fileOpts, thread, layerName+".star", script, predeclared)
	if err != nil {
		return nil, fmt.Errorf("layer %q: %w", layerName, err)
	}

	result, ok := globals[resultVar]
	if !ok {
		return nil, fmt.Errorf("layer %q: dynamic layer must assign its output to a global named %q", layerName, resultVar)
	}
	goVal, err := starlarkToGo(result)
	if err != nil {
		return nil, fmt.Errorf("layer %q: %w", layerName, err)
	}
	m, ok := goVal.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("layer %q: %q must be a dict, got %T", layerName, resultVar, goVal)
	}
	return m, nil
}
