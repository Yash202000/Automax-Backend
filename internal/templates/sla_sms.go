package templates

import (
	"fmt"
	"time"
)

func BuildSLABreachSMS(firstName string, lastName string, incidentCount int, classificationName string, slaPageURL string) string {

	return fmt.Sprintf(
		"Dear %s %s,\n\n"+
			"This is an automated SLA breach notification.\n\n"+
			"%d incident(s) exceeded SLA\n"+
			"Classification: %s\n\n"+
			"View Incidents:\n%s\n\n"+
			"- SLA Monitoring System",
		firstName,
		lastName,
		incidentCount,
		classificationName,
		slaPageURL,
	)
}

func BuildSLABreachSMSHTML(firstName string, lastName string, incidentCount int, classificationName string, slaPageURL string) string {

	reportDate := time.Now().Format("02 Jan 2006, 15:04")

	return fmt.Sprintf(`
<div style="font-family: Arial, sans-serif; line-height:1.6; color:#333;">
    
    <p>Dear <strong>%s %s</strong>,</p>

    <p>
        This is an automated 
        <strong style="color:#d9534f;">SLA breach notification</strong>.
    </p>

    <p>
        <strong>%d incident(s)</strong> exceeded SLA under 
        "<strong>%s</strong>" classification.
    </p>

    <p>
        <a href="%s"
           target="_blank"
           style="background:#007bff;
                  color:#fff;
                  padding:10px 15px;
                  text-decoration:none;
                  border-radius:5px;
                  font-weight:bold;">
            View SLA Breached Incidents
        </a>
    </p>

    <hr>

    <p style="font-size:12px;color:#777;">
        Report generated on %s<br>
        SLA Monitoring System
    </p>

</div>
`,
		firstName,
		lastName,
		incidentCount,
		classificationName,
		slaPageURL,
		reportDate,
	)
}
