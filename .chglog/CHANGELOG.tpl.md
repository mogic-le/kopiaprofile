{{- range .Versions }}
<a name="v{{.Version}}"></a>
## {{.Version}} ({{.Date}})
{{ range .CommitGroups -}}
### {{.Name}}
{{ range .Commits -}}
* {{ if .Scope }}**{{.Scope}}**: {{ end }}{{.Subject}} ([{{.Hash.Short}}]({{.CommitURL}}))
{{ end }}
{{ end -}}
{{ end -}}
