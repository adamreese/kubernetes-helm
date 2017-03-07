/*
Copyright 2016 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"io"
	"text/template"
	"time"

	"github.com/golang/protobuf/ptypes/timestamp"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/timeconv"
)

var printReleaseTemplate = `NAME: {{.Release.Name}}
REVISION: {{.Release.Version}}
RELEASED: {{.ReleaseDate}}
CHART: {{.Release.Chart.Metadata.Name}}-{{.Release.Chart.Metadata.Version}}
USER-SUPPLIED VALUES:
{{.Release.Config.Raw}}
COMPUTED VALUES:
{{.ComputedValues}}
HOOKS:
{{- range .Release.Hooks }}
---
# {{.Name}}
{{.Manifest}}
{{- end }}
MANIFEST:
{{.Release.Manifest}}
`

type releaseJSON struct {
	Name      string          `json:"name"`
	Info      infoJSON        `json:"info,omitempty"`
	Chart     chartJSON       `json:"chart"`
	Config    string          `json:"config,omitempty"`
	Manifest  string          `json:"manifest,omitempty"`
	Hooks     []*release.Hook `json:"hooks,omitempty"`
	Version   int32           `json:"version"`
	Namespace string          `json:"namespace,omitempty"`
}

func newReleaseJSON(rel *release.Release) *releaseJSON {
	return &releaseJSON{
		Name: rel.Name,
		Info: newInfoJSON(rel.Info),
		Chart: chartJSON{
			Name:    rel.Chart.Metadata.Name,
			Version: rel.Chart.Metadata.Version,
		},
		Config:    rel.Config.Raw,
		Manifest:  rel.Manifest,
		Hooks:     rel.Hooks,
		Version:   rel.Version,
		Namespace: rel.Namespace,
	}
}

type infoJSON struct {
	Status        string     `json:"status,omitempty"`
	FirstDeployed *time.Time `json:"first_deployed,omitempty"`
	LastDeployed  *time.Time `json:"last_deployed,omitempty"`
	Deleted       *time.Time `json:"deleted,omitempty"`
	Description   string     `json:"description,omitempty"`
	Notes         string     `json:"notes,omitempty"`
}

func newInfoJSON(info *release.Info) infoJSON {
	return infoJSON{
		Status:        info.Status.Code.String(),
		Notes:         info.Status.Notes,
		FirstDeployed: convertTimestamp(info.FirstDeployed),
		LastDeployed:  convertTimestamp(info.LastDeployed),
		Deleted:       convertTimestamp(info.Deleted),
		Description:   info.Description,
	}
}

type chartJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func convertTimestamp(ts *timestamp.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := time.Unix(ts.Seconds, int64(ts.Nanos))
	return &t
}

func printRelease(out io.Writer, rel *release.Release) error {
	if rel == nil {
		return nil
	}

	printJSON(out, newReleaseJSON(rel))

	return nil

	cfg, err := chartutil.CoalesceValues(rel.Chart, rel.Config)
	if err != nil {
		return err
	}
	cfgStr, err := cfg.YAML()
	if err != nil {
		return err
	}

	data := map[string]interface{}{
		"Release":        rel,
		"ComputedValues": cfgStr,
		"ReleaseDate":    timeconv.Format(rel.Info.LastDeployed, time.ANSIC),
	}
	return tpl(printReleaseTemplate, data, out)
}

func tpl(t string, vals map[string]interface{}, out io.Writer) error {
	tt, err := template.New("_").Parse(t)
	if err != nil {
		return err
	}
	return tt.Execute(out, vals)
}

func printJSON(out io.Writer, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	out.Write(data)
	return nil
}
