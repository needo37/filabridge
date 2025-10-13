{{ range .Versions }}
## [{{ .Tag.Name }}] - {{ datetime "2006-01-02" .Tag.Date }}

{{ range .CommitGroups }}
### {{ .Title }}
{{ range .Commits }}
- {{ .Subject }}
{{ end }}
{{ end }}

{{ end }}