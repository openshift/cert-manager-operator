package deployment

import (
	"os"
)

const (
	operatorName      = "cert-manager"
	operandNamePrefix = ""
	conditionsPrefix  = "CertManager"
)

var targetVersion = os.Getenv("OPERAND_CERT_MANAGER_IMAGE_VERSION")
