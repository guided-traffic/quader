package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The codes of a [SettingViolation]: the repository model answers them as the
// codes of its 422 field errors (R-REPO-008).
const (
	// SettingInvalid is a value the schema refuses: a wrong type, an unknown member,
	// a value outside an enum or a range.
	SettingInvalid = "invalid_value"
	// SettingNotApplicable is a member of the settings that the class of the
	// repository does not use (the schema's x-quader-classes).
	SettingNotApplicable = "not_applicable"
)

// SettingViolation is one finding of [Entry.ValidateSettings].
type SettingViolation struct {
	// Pointer is the JSON Pointer of the member inside the settings object ("" for the
	// object itself); the caller prefixes the name of the settings member.
	Pointer string
	Code    string
	Detail  string
}

// settingsSchema is the compiled form of [Facts.SettingsSchema].
type settingsSchema struct {
	schema *jsonschema.Schema
	// classes holds the classes of the members that name some (x-quader-classes), by the
	// member's JSON Pointer inside the settings object ("/latestGuard", "/audit/providerURL");
	// a member that names none applies to every class its parent applies to.
	classes map[string][]Class
	// valueClasses holds the classes of the values of an enum that name some
	// (x-quader-enum-classes), by the member's pointer and the value as text: npm's audit
	// mode forward applies to remote and virtual repositories only.
	valueClasses map[string]map[string][]Class
}

// collectClasses reads x-quader-classes and x-quader-enum-classes of every member below
// the schema object decl, at any depth, into out; pointer is decl's own pointer.
func collectClasses(id ID, decl map[string]any, pointer string, out *settingsSchema) error {
	props, _ := decl["properties"].(map[string]any)
	for name, p := range props {
		member, _ := p.(map[string]any)
		at := pointer + "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
		if list, ok := member["x-quader-classes"].([]any); ok {
			cs, err := classList(id, at, list)
			if err != nil {
				return err
			}
			out.classes[at] = cs
		}
		if byValue, ok := member["x-quader-enum-classes"].(map[string]any); ok {
			out.valueClasses[at] = map[string][]Class{}
			for value, v := range byValue {
				list, _ := v.([]any)
				cs, err := classList(id, at+" "+value, list)
				if err != nil {
					return err
				}
				out.valueClasses[at][value] = cs
			}
		}
		if err := collectClasses(id, member, at, out); err != nil {
			return err
		}
	}
	return nil
}

// classList is the classes of one annotation; a class that does not exist refuses the schema.
func classList(id ID, at string, list []any) ([]Class, error) {
	out := make([]Class, 0, len(list))
	for _, v := range list {
		c, _ := v.(string)
		if !Class(c).Valid() {
			return nil, fmt.Errorf("%q: the settings member %q applies to the class %q, which does not exist", id, at, c)
		}
		out = append(out, Class(c))
	}
	return out, nil
}

// compileSettings compiles the JSON Schema 2020-12 of a format's settings member
// (R-ARCH-008). The root must be an object that refuses members it does not list
// (additionalProperties false), because a settings document that silently drops a
// misspelt member leaves the operator believing it is in effect (R-REPO-008).
func compileSettings(id ID, raw []byte) (*settingsSchema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%q: the settings schema is no JSON: %w", id, err)
	}
	root, ok := doc.(map[string]any)
	if !ok || root["type"] != "object" || root["additionalProperties"] != false {
		return nil, fmt.Errorf(`%q: the settings schema must be an object schema with "additionalProperties": false`, id)
	}
	out := &settingsSchema{classes: map[string][]Class{}, valueClasses: map[string]map[string][]Class{}}
	if err := collectClasses(id, root, "", out); err != nil {
		return nil, err
	}
	url := "quader:formats/" + string(id) + "/settings.json"
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource(url, doc); err != nil {
		return nil, fmt.Errorf("%q: the settings schema is refused: %w", id, err)
	}
	if out.schema, err = c.Compile(url); err != nil {
		return nil, fmt.Errorf("%q: the settings schema does not compile: %w", id, err)
	}
	return out, nil
}

// HasSettings reports whether the format declares a settings schema.
func (e Entry) HasSettings() bool { return e.settings != nil }

// ValidateSettings checks the settings member of a repository document of the given
// class against the format's schema (R-ARCH-008, R-REPO-008): every violation is
// reported, not the first. A member whose schema names classes (x-quader-classes), at any
// depth, is not applicable to a repository of another class, and so is a value of an enum
// whose schema names classes for it (x-quader-enum-classes). A format without a settings
// schema takes no settings: any content is a violation. An empty member is valid.
func (e Entry) ValidateSettings(class Class, raw json.RawMessage) []SettingViolation {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if e.settings == nil {
		return []SettingViolation{{Code: SettingNotApplicable, Detail: fmt.Sprintf("the %s format has no settings", e.ID)}}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return []SettingViolation{{Code: SettingInvalid, Detail: "must be a JSON object"}}
	}
	var out []SettingViolation
	if err := e.settings.schema.Validate(inst); err != nil {
		var verr *jsonschema.ValidationError
		if !errors.As(err, &verr) {
			return []SettingViolation{{Code: SettingInvalid, Detail: err.Error()}}
		}
		for _, u := range verr.BasicOutput().Errors {
			if u.Error != nil {
				out = append(out, SettingViolation{Pointer: u.InstanceLocation, Code: SettingInvalid, Detail: u.Error.String()})
			}
		}
	}
	return append(out, e.settings.notApplicable(class, inst, "")...)
}

// notApplicable reports the members of the instance value v at pointer, at any depth, that
// the class does not use, and the values of an enum it does not use; a member that is not
// applicable is reported once, not again for what it holds.
func (s *settingsSchema) notApplicable(class Class, v any, pointer string) []SettingViolation {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []SettingViolation
	for _, name := range names {
		at := pointer + "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
		if cs, scoped := s.classes[at]; scoped && !hasClass(cs, class) {
			out = append(out, SettingViolation{
				Pointer: at, Code: SettingNotApplicable,
				Detail: "the setting applies to " + joinClasses(cs) + " repositories",
			})
			continue
		}
		if value, isText := obj[name].(string); isText {
			if cs, scoped := s.valueClasses[at][value]; scoped && !hasClass(cs, class) {
				out = append(out, SettingViolation{
					Pointer: at, Code: SettingNotApplicable,
					Detail: "the value " + value + " applies to " + joinClasses(cs) + " repositories",
				})
			}
		}
		out = append(out, s.notApplicable(class, obj[name], at)...)
	}
	return out
}

func hasClass(list []Class, c Class) bool {
	for _, k := range list {
		if k == c {
			return true
		}
	}
	return false
}

func joinClasses(list []Class) string {
	names := make([]string, len(list))
	for i, c := range list {
		names[i] = string(c)
	}
	return strings.Join(names, " and ")
}
