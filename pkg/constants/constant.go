package constants

type stringKey string

var AUTH_DATA = struct {
	UserID    stringKey
	UserEmail stringKey
	Role      stringKey
}{
	UserID:    "user_id",
	UserEmail: "user_email",
	Role:      "role",
}

var USER_ROLE = struct {
	SUPER_ADMIN string
	ADMIN       string
	USER        string
	CITIZEN     string
	AGENT       string
}{
	SUPER_ADMIN: "super_admin",
	ADMIN:       "admin",
	USER:        "user",
	CITIZEN:     "citizen",
	AGENT:       "agent",
}

var CLIENT_CODE = struct {
	VIUSIONAL    string
	EPM940       string
	IVR_INST_SMS string
	VD2          string
}{
	VIUSIONAL:    "Viusional",
	EPM940:       "EPM940",
	IVR_INST_SMS: "IVRINSTSMS",
	VD2:          "VD2",
}

var INCIDENT_SOURCE = struct {
	WEB       string
	MOBILE    string
	IVR       string
	WHATSAPP  string
	FACEBOOK  string
	TWITTER   string
	EMAIL     string
	EPMPORTAL string
	VIUSIONAL string
}{
	WEB:       "web",
	MOBILE:    "mobile",
	IVR:       "ivr",
	WHATSAPP:  "whatsapp",
	FACEBOOK:  "facebook",
	TWITTER:   "twitter",
	EMAIL:     "email",
	EPMPORTAL: "epmportal",
	VIUSIONAL: "viusional",
}

var PREFIX = struct {
	IVR_EMAIL stringKey
	PHONE     stringKey
}{
	IVR_EMAIL: "ivr_email",
	PHONE:     "phone",
}

var APP = struct {
	NAME   string
	DOMAIN string
}{
	NAME:   "Automax",
	DOMAIN: "automax.com",
}

var ROLES = struct {
	SUPER_ADMIN        string
	ADMIN              string
	USER               string
	CITIZEN            string
	AGENT              string
	DEPARTMENT_MANAGER string
}{
	SUPER_ADMIN:        "super_admin",
	ADMIN:              "admin",
	USER:               "user",
	CITIZEN:            "citizen",
	AGENT:              "agent",
	DEPARTMENT_MANAGER: "department_manager",
}
