package payment

import "errors"

// repository holds the payment commander's external dependencies.
type repository struct {
	provider Provider
}

type external func(*repository)

// WithProvider injects the payment service provider the commands authorise and
// capture against. Pass an adapter from the composition root — the entity must
// not name a specific provider (see interfaces.go).
func WithProvider(p Provider) external {
	return func(r *repository) {
		r.provider = p
	}
}

func NewRepository(es ...external) repository {
	r := repository{
		// A missing wire should say so rather than panic on a nil interface
		// three frames down, inside a payment attempt.
		provider: unconfiguredProvider{},
	}
	for _, with := range es {
		with(&r)
	}
	return r
}

// ErrNoProvider is returned by every command when no Provider was wired.
var ErrNoProvider = errors.New("payment: no provider configured (use payment.WithProvider)")

type unconfiguredProvider struct{}

func (unconfiguredProvider) CreatePayment(PaymentRequest) (PaymentCreated, error) {
	return PaymentCreated{}, ErrNoProvider
}

func (unconfiguredProvider) GetAuthorization(string) (Authorization, error) {
	return Authorization{}, ErrNoProvider
}

func (unconfiguredProvider) CapturePayment(string, Amount) error {
	return ErrNoProvider
}

var _ Provider = unconfiguredProvider{}
