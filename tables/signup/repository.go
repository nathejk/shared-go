package signup

type repository struct {
	sms  SmsSender
	mail Mailer
}

type service func(*repository)

func WithSms(s SmsSender) service {
	return func(r *repository) {
		r.sms = s
	}
}
func WithMailer(s Mailer) service {
	return func(r *repository) {
		r.mail = s
	}
}

func NewRepository(services ...service) repository {
	r := repository{}
	for _, with := range services {
		with(&r)
	}
	return r
}
