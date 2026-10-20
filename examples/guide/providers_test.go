package guide_test

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/omyo/gonest"
)

// docs:providers-kinds

// Clock is an interface token.
type Clock interface{ Now() string }

type fixedClock struct{}

func (fixedClock) Now() string { return "2026-01-01" }

// NewFixedClock returns a concrete type; Bind registers it under Clock.
func NewFixedClock() *fixedClock { return &fixedClock{} }

// DB stands for a database handle.
type DB struct{ DSN string }

// Settings is registered with Value.
type Settings struct{ PrimaryDSN, ReplicaDSN string }

// ReportDeps is a parameter object: Named providers and optional
// dependencies are only available through one.
type ReportDeps struct {
	gonest.In
	Primary *DB          `name:"primary"`
	Replica *DB          `name:"replica"`
	Log     *slog.Logger `optional:"true"`
	Clock   Clock
}

// ReportService depends on two DBs of the same type.
type ReportService struct{ d ReportDeps }

// NewReportService is a constructor with a parameter object.
func NewReportService(d ReportDeps) *ReportService { return &ReportService{d: d} }

// ReportsModule shows every provider kind.
var ReportsModule = &gonest.Module{
	Name: "reports",
	Providers: []gonest.Provider{
		gonest.Value(&Settings{PrimaryDSN: "db1", ReplicaDSN: "db2"}), // useValue
		gonest.Bind[Clock](NewFixedClock),                             // useClass
		gonest.Factory[*DB](func(s *Settings) *DB { // useFactory
			return &DB{DSN: s.PrimaryDSN}
		}, gonest.Named("primary")),
		gonest.Factory[*DB](func(s *Settings) (*DB, error) {
			if s.ReplicaDSN == "" {
				return nil, errors.New("no replica configured")
			}
			return &DB{DSN: s.ReplicaDSN}, nil
		}, gonest.Named("replica")),
		gonest.Provide(NewReportService), // @Injectable()
	},
	Exports: []gonest.Token{gonest.TokenOf[*ReportService]()},
}

// docs:end

func Example_providerKinds() {
	app, err := gonest.New(&gonest.Module{Name: "app", Imports: []*gonest.Module{ReportsModule}}, quiet()...)
	if err != nil {
		fmt.Println(err)
		return
	}
	// docs:get
	svc, err := gonest.Get[*ReportService](app) // for main functions and tests
	// docs:end
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(svc.d.Primary.DSN, svc.d.Replica.DSN, svc.d.Clock.Now(), svc.d.Log != nil)
	// Output: db1 db2 2026-01-01 true
}

// docs:transient

// Counter counts how many instances were built.
type Counter struct{ ID int }

// Consumers receive their own Counter because it is Transient.
type Consumers struct {
	gonest.In
	A *ConsumerA
	B *ConsumerB
}

// ConsumerA depends on a transient Counter.
type ConsumerA struct{ C *Counter }

// ConsumerB depends on a transient Counter.
type ConsumerB struct{ C *Counter }

func Example_transient() {
	built := 0
	mod := &gonest.Module{
		Name: "app",
		Providers: []gonest.Provider{
			gonest.Provide(func() *Counter { built++; return &Counter{ID: built} }, gonest.Transient),
			gonest.Provide(func(c *Counter) *ConsumerA { return &ConsumerA{C: c} }),
			gonest.Provide(func(c *Counter) *ConsumerB { return &ConsumerB{C: c} }),
		},
	}
	app, err := gonest.New(mod, quiet()...)
	if err != nil {
		fmt.Println(err)
		return
	}
	a := gonest.MustGet[*ConsumerA](app)
	b := gonest.MustGet[*ConsumerB](app)
	fmt.Println(a.C.ID != b.C.ID)
	// Output: true
}

// docs:end

func Example_resolveError() {
	// docs:resolve-error
	type Mailer struct{}
	type SignupService struct{ m *Mailer }
	mod := &gonest.Module{
		Name: "signup",
		Providers: []gonest.Provider{
			gonest.Provide(func(m *Mailer) *SignupService { return &SignupService{m: m} }),
		},
	}
	_, err := gonest.New(mod)
	fmt.Println(err)
	// docs:end

	var re *gonest.ResolveError
	fmt.Println(errors.As(err, &re))
	// Output:
	// gonest: cannot resolve *guide_test.SignupService -> *guide_test.Mailer in module signup: no provider (hint: register a provider for *guide_test.Mailer in this module or import a module that exports it)
	// true
}
