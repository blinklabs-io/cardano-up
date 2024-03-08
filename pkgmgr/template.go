// Copyright 2024 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pkgmgr

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

type Template struct {
	tmpl     *template.Template
	baseVars map[string]any
}

func NewTemplate(baseVars map[string]any) *Template {
	return &Template{
		tmpl:     template.New("main").Funcs(sprig.FuncMap()),
		baseVars: baseVars,
	}
}

func (t *Template) Render(tmplBody string, extraVars map[string]any) (string, error) {
	// Build our vars
	tmpVars := map[string]any{}
	for k, v := range t.baseVars {
		tmpVars[k] = v
	}
	for k, v := range extraVars {
		tmpVars[k] = v
	}
	// Parse template body
	tmpl, err := t.tmpl.Parse(tmplBody)
	if err != nil {
		return "", err
	}
	// Render template
	outBuffer := bytes.NewBuffer(nil)
	if err := tmpl.Execute(outBuffer, tmpVars); err != nil {
		return "", err
	}
	return outBuffer.String(), nil
}

// WithVars creates a copy of the Template with the extra variables added to the original base variables
func (t *Template) WithVars(extraVars map[string]any) *Template {
	tmpVars := map[string]any{}
	for k, v := range t.baseVars {
		tmpVars[k] = v
	}
	for k, v := range extraVars {
		tmpVars[k] = v
	}
	tmpl := NewTemplate(tmpVars)
	return tmpl
}

func (t *Template) EvaluateCondition(condition string, extraVars map[string]any) (bool, error) {
	tmpl := fmt.Sprintf(
		`{{ if %s }}true{{ else }}false{{ end }}`,
		condition,
	)
	rendered, err := t.Render(tmpl, extraVars)
	if err != nil {
		return false, err
	}
	if rendered == `true` {
		return true, nil
	}
	return false, nil
}
