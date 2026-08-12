package resolve

import (
	"fmt"

	"go.starlark.net/starlark"
)

// goToStarlark converts a Go value produced by YAML/JSON unmarshaling
// (map[string]any, []any, string, bool, int, int64, float64, nil) into the
// equivalent Starlark value, so it can be exposed to a dynamic layer.
func goToStarlark(v any) (starlark.Value, error) {
	switch t := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(t), nil
	case string:
		return starlark.String(t), nil
	case int:
		return starlark.MakeInt(t), nil
	case int64:
		return starlark.MakeInt64(t), nil
	case float64:
		return starlark.Float(t), nil
	case []any:
		elems := make([]starlark.Value, len(t))
		for i, e := range t {
			sv, err := goToStarlark(e)
			if err != nil {
				return nil, err
			}
			elems[i] = sv
		}
		return starlark.NewList(elems), nil
	case map[string]any:
		d := starlark.NewDict(len(t))
		for k, e := range t {
			sv, err := goToStarlark(e)
			if err != nil {
				return nil, err
			}
			if err := d.SetKey(starlark.String(k), sv); err != nil {
				return nil, err
			}
		}
		return d, nil
	default:
		return nil, fmt.Errorf("resolve: unsupported Go type %T for starlark conversion", v)
	}
}

// starlarkToGo converts a Starlark value back into the plain Go
// representation used by the resolver (mirrors goToStarlark).
func starlarkToGo(v starlark.Value) (any, error) {
	switch t := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(t), nil
	case starlark.String:
		return string(t), nil
	case starlark.Int:
		i, ok := t.Int64()
		if !ok {
			return nil, fmt.Errorf("resolve: starlark int %s overflows int64", t.String())
		}
		return i, nil
	case starlark.Float:
		return float64(t), nil
	case *starlark.List:
		out := make([]any, 0, t.Len())
		for e := range t.Elements() {
			gv, err := starlarkToGo(e)
			if err != nil {
				return nil, err
			}
			out = append(out, gv)
		}
		return out, nil
	case *starlark.Dict:
		out := make(map[string]any, t.Len())
		for k, val := range t.Entries() {
			ks, ok := starlark.AsString(k)
			if !ok {
				return nil, fmt.Errorf("resolve: starlark dict key %v is not a string", k)
			}
			gv, err := starlarkToGo(val)
			if err != nil {
				return nil, err
			}
			out[ks] = gv
		}
		return out, nil
	default:
		return nil, fmt.Errorf("resolve: unsupported starlark type %s for go conversion", v.Type())
	}
}
