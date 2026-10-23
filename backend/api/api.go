// Package api elle yazilmis OpenAPI 3.1 sozlesmesini binary'ye gomer (GET /openapi.yaml).
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPI []byte
