# Changelog

All notable changes to FilaBridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
