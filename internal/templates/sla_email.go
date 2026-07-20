package templates

import (
	"fmt"
	"time"
)

func BuildSLABreachEmail(
	firstName string,
	lastName string,
	incidentCount int,
	classificationName string,
	slaPageURL string,
) (subject string, body string) {

	reportDate := time.Now().Format("02 Jan 2006, 15:04")
	fileDate := time.Now().Format("2006-01-02")

	subject = fmt.Sprintf(
		"SLA Breach Report — %d Incident(s) Exceeded SLA [%s]",
		incidentCount,
		classificationName,
	)

	body = fmt.Sprintf(`
<html>
<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333;">
    
    <p>Dear <strong>%s %s</strong>,</p>

    <p>
        This is an automated <strong style="color: #d9534f;">SLA breach notification</strong>.
    </p>

    <p>
        You have <strong>%d open incident(s)</strong> that have exceeded their SLA deadline
        under the classification "<strong>%s</strong>".
    </p>

    <p>
         <a href="%s" 
              style="background-color: #007bff;
                     color: #ffffff;
                     padding: 10px 15px;
                     text-decoration: none;
                     border-radius: 5px;
                     font-weight: bold;
                     display: inline-block;
                     margin-bottom: 10px;">
            View SLA Breached Incidents
        </a>
    </p>

    <hr>

    <p><strong>Report Details:</strong></p>
    <ul>
        <li><strong>Report Date:</strong> %s</li>
        <li><strong>Classification:</strong> %s</li>
        <li><strong>Total Breached:</strong> %d incident(s)</li>
    </ul>

    <p>
        A complete breach report is attached to this email 
        (<strong>sla_breach_report_%s.csv</strong>).
    </p>

    <hr>

    <p style="font-size: 12px; color: #777;">
        This is an automated notification from the SLA Monitoring System.<br>
        Please do not reply to this email.
    </p>

</body>
</html>
`,
		firstName,
		lastName,
		incidentCount,
		classificationName,
		slaPageURL,
		reportDate,
		classificationName,
		incidentCount,
		fileDate,
	)

	return
}
