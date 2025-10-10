# Changelog

{{ range .Versions }}
## [{{ .Tag.Name }}] - {{ datetime "2006-01-02" .Tag.Date }}
{{ if .Tag.Previous }}
[Full Changelog]({{ $.Info.RepositoryURL }}/compare/{{ .Tag.Previous.Name }}...{{ .Tag.Name }})
{{ end }}

{{ range .CommitGroups }}
### {{ .Title }}
{{ range .Commits }}
- {{ .Subject }}{{ if .Scope }} ({{ .Scope }}){{ end }}
{{ end }}

{{ end }}
{{ end }}