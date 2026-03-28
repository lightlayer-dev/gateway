package configs

import _ "embed"

// GatewayYAML is the default gateway.yaml template embedded in the binary.
//
//go:embed gateway.yaml
var GatewayYAML []byte
