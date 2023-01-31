package deployment

const (
	operatorName      = "cert-manager"
	operandNamePrefix = ""
	conditionsPrefix  = "CertManager"

	certmanagerControllerDeployment = "cert-manager"
	certmanagerWebhookDeployment    = "cert-manager-webhook"
	certmanagerCAinjectorDeployment = "cert-manager-cainjector"
)
