package conf

type SIP struct {
	Port     int
	ID       string
	Domain   string
	Password string
}

type Media struct {
	IP           string
	HTTPPort     int
	Secret       string
	Type         string
	WebHookIP    string
	RTPPortRange string
	SDPIP        string
}

type Bootstrap struct {
	Sip   SIP
	Media Media
}
