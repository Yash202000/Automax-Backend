package templates

import "fmt"

func BuildSLABreachSMS(
	incidentCount int,
	classificationName string,
	slaPageURL string,
) string {

	return fmt.Sprintf(
		"SLA Alert: %d incident(s) exceeded SLA (Classification: %s). View: %s",
		incidentCount,
		classificationName,
		slaPageURL,
	)
}
