package mapadapter

import (
	"fmt"
	"sdmm/internal/dmapi/dmenv"
)

func EnvironmentHash(environment *dmenv.Dme) (string, error) {
	if environment == nil {
		return "", fmt.Errorf("hash environment: environment is nil")
	}
	return environment.EnvironmentHash()
}
