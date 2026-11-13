package console

import (
	"reflect"
	"strings"
	"testing"
)

// fields walks every string (and every string of a string array) of a table
// with its field name.
func fields(t *testing.T, s strs) map[string]string {
	t.Helper()
	out := map[string]string{}
	v := reflect.ValueOf(s)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			out[name] = f.String()
		case reflect.Array:
			for j := 0; j < f.Len(); j++ {
				out[name+"["+string(rune('0'+j))+"]"] = f.Index(j).String()
			}
		default:
			t.Fatalf("field %s has an unexpected kind %s", name, f.Kind())
		}
	}
	return out
}

func TestEveryTextExistsInBothLanguages(t *testing.T) {
	en, it := fields(t, en), fields(t, it)
	if len(en) != len(it) {
		t.Fatalf("English has %d texts, Italian %d", len(en), len(it))
	}
	for name, text := range en {
		if strings.TrimSpace(text) == "" {
			t.Errorf("English %s is empty", name)
		}
		if strings.TrimSpace(it[name]) == "" {
			t.Errorf("Italian %s is empty", name)
		}
	}
}

func TestTemplatesKeepTheirVerbs(t *testing.T) {
	en, it := fields(t, en), fields(t, it)
	for name, text := range en {
		if a, b := strings.Count(text, "%s"), strings.Count(it[name], "%s"); a != b {
			t.Errorf("%s: English has %d %%s, Italian %d", name, a, b)
		}
	}
}

func TestHelpLinesKeepTheirKeys(t *testing.T) {
	// The keys that do something are the same letters in both languages.
	for _, k := range []string{"r ", "p ", "q ", "y ", "n "} {
		join := en.HelpMain + en.HelpConfirm
		joinIt := it.HelpMain + it.HelpConfirm
		if strings.Contains(join, k) != strings.Contains(joinIt, k) {
			t.Errorf("the key %q is documented in only one language", strings.TrimSpace(k))
		}
	}
}

func TestUseLocale(t *testing.T) {
	defer func() { T = en }()
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for name, tc := range map[string]struct {
		env  map[string]string
		want strs
	}{
		"nothing set":                    {map[string]string{}, en},
		"English":                        {map[string]string{"LANG": "en_US.UTF-8"}, en},
		"Italian":                        {map[string]string{"LANG": "it_IT.UTF-8"}, it},
		"bare it":                        {map[string]string{"LANG": "it"}, it},
		"LC_ALL wins over LANG":          {map[string]string{"LC_ALL": "en_GB.UTF-8", "LANG": "it_IT.UTF-8"}, en},
		"LC_ALL Italian over English":    {map[string]string{"LC_ALL": "it_IT.UTF-8", "LANG": "en_US.UTF-8"}, it},
		"LC_MESSAGES over LANG":          {map[string]string{"LC_MESSAGES": "it_CH", "LANG": "en_US"}, it},
		"a language that only starts it": {map[string]string{"LANG": "ita_XX"}, en},
		"C":                              {map[string]string{"LANG": "C"}, en},
	} {
		UseLocale(get(tc.env))
		if !reflect.DeepEqual(T, tc.want) {
			t.Errorf("%s: wrong language", name)
		}
	}
	// It always resets: an Italian call, then an empty environment.
	UseLocale(get(map[string]string{"LANG": "it_IT"}))
	UseLocale(get(nil))
	if !reflect.DeepEqual(T, en) {
		t.Error("UseLocale does not reset to English")
	}
}
