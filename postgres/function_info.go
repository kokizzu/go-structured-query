package sq

import "strings"

// FunctionInfo is struct that implements the Table/Field interface, containing
// all the information needed to call itself a Table/Field. It is meant to be
// embedded in arbitrary structs to also transform them into valid
// Tables/Fields.
type FunctionInfo struct {
	Schema    string
	Name      string
	Alias     string
	Arguments []interface{}
}

// AppendSQL adds the fully qualified function call into the buffer.
func (f *FunctionInfo) AppendSQL(buf *strings.Builder, args *[]interface{}, params map[string]int) {
	f.AppendSQLExclude(buf, args, nil, nil)
}

// AppendSQLExclude adds the fully qualified function call into the buffer.
func (f *FunctionInfo) AppendSQLExclude(buf *strings.Builder, args *[]interface{}, params map[string]int, excludedTableQualifiers []string) {
	if f == nil {
		return
	}
	var format string
	if f.Schema != "" {
		if strings.ContainsAny(f.Schema, " \t") {
			format = `"` + f.Schema + `".`
		} else {
			format = f.Schema + "."
		}
	}
	switch len(f.Arguments) {
	case 0:
		format = format + f.Name + "()"
	default:
		format = format + f.Name + "(?" + strings.Repeat(", ?", len(f.Arguments)-1) + ")"
	}
	expandValues(buf, args, excludedTableQualifiers, format, f.Arguments)
}

// Functionf creates a new FunctionInfo.
func Functionf(name string, args ...interface{}) *FunctionInfo {
	return &FunctionInfo{
		Name:      name,
		Arguments: args,
	}
}

// GetAlias implements the Table interface. It returns the alias of the
// FunctionInfo.
func (f *FunctionInfo) GetAlias() string {
	return f.Alias
}

// GetName implements the Table interface. It returns the name of the
// FunctionInfo.
func (f *FunctionInfo) GetName() string {
	return f.Name
}
