# Coming from NestJS

Gonest keeps the NestJS vocabulary and the request pipeline, and replaces
decorators with values the compiler can check. Each section shows the NestJS
code first, then the Gonest code from the example application.

## Quick map

| NestJS | Gonest |
|---|---|
| `@Module({ imports, providers, controllers, exports })` | `&gonest.Module{Name, Imports, Providers, Controllers, Exports}` |
| `exports: [OtherModule]` | `ReExports: []*gonest.Module{OtherModule}` |
| `@Global()` | `Global: true` |
| `ConfigModule.forRoot()` (dynamic module) | a function returning `*gonest.Module` (`config.ForRoot[T](...)`) |
| `@Injectable()` + constructor | constructor function `func NewX(deps...) *X` + `gonest.Provide(NewX)` |
| `useValue` / `useClass` / `useFactory` | `gonest.Value` / `gonest.Bind[I]` / `gonest.Factory[T]` |
| `@Inject('TOKEN')`, `@Optional()` | parameter object embedding `gonest.In`, tags `name:"..."`, `optional:"true"` |
| `Scope.TRANSIENT` / `Scope.REQUEST` | `gonest.Transient` / not supported (use request values) |
| `@Controller('users')` | `r.Prefix("/users")` in `Routes(r gonest.Router)` |
| `@Get(':id')`, `@Post()` ... | `r.GET("/:id", gonest.Handle(c.FindOne))`, `r.POST(...)` |
| `@Param('id')`, `@Query()`, `@Headers()`, `@Body()` | one input struct: `path:"id"`, `query:"..."`, `header:"..."`, `json:"..."` |
| class-validator decorators + `ValidationPipe` | `validate:"..."` tags, always on (validator/v10) |
| `@HttpCode(204)` | `gonest.Status(204)`, or return `gonest.NoContent` |
| `@Res()` | `gonest.HandleRaw(func(ctx *gonest.Ctx) error)` |
| `@UseGuards`, `@UseInterceptors`, `@UsePipes`, `@UseFilters` | `gonest.UseGuards(...)` etc., on a route or via `r.Use` |
| `APP_GUARD`, `APP_INTERCEPTOR`, `APP_PIPE`, `APP_FILTER` | `gonest.AppGuard(ctor)`, `AppInterceptor`, `AppPipe`, `AppFilter` |
| `SetMetadata` + `Reflector` | `gonest.SetMetadata(k, v)` + `ctx.Route().Metadata(k)` |
| `ExecutionContext` / `ArgumentsHost` | `*gonest.Ctx` |
| `Observable` in interceptors | `next()` returns `(any, error)` |
| `throw new NotFoundException()` | `return gonest.NotFound("...")` |
| `NestMiddleware` + `configure(consumer)` | gin middleware + `Module.Configure` |
| `OnModuleInit` ... `OnApplicationShutdown` | same names, `(ctx context.Context) error` |
| `app.setGlobalPrefix('api')` | `gonest.WithGlobalPrefix("/api")` |
| `app.enableShutdownHooks()` | always on with `app.Listen` |
| `@nestjs/swagger` decorators | none needed: `openapi.Module`, plus `gonest.Summary`, `Tags`, `Responds`, `doc`/`example` tags |
| `Test.createTestingModule().overrideProvider().useValue()` | `gonesttest.New(t, mod).OverrideProvider(gonest.Value(...)).Compile()` |
| `nest new`, `nest g` | `gonest new`, `gonest g` |

## Modules

```ts
@Module({
  imports: [AuthModule, TenantsModule],
  providers: [{ provide: ProjectsRepository, useClass: MemoryProjectsRepository }, ProjectsService],
  controllers: [ProjectsController],
})
export class ProjectsModule {}
```

<!-- snippet: examples/saas/internal/projects/module.go#projects-module -->
```go
// Module wires the projects feature. Repository is bound to the in-memory
// implementation (NestJS useClass); tenants and auth provide the exported
// TenantService and guards.
var Module = &gonest.Module{
	Name:    "projects",
	Imports: []*gonest.Module{auth.Module, tenants.Module},
	Providers: []gonest.Provider{
		gonest.Bind[Repository](NewMemoryRepository),
		gonest.Provide(NewService),
	},
	Controllers: []any{NewProjectsController},
}
```

## Providers and injection

```ts
@Injectable()
export class ProjectsService {
  constructor(
    private readonly repo: ProjectsRepository,
    private readonly tenants: TenantsService,
    private readonly logger: Logger,
  ) {}
}
```

<!-- snippet: examples/saas/internal/projects/service.go#service-ctor -->
```go
// NewService is the provider constructor. Repository is an interface bound
// in the module; *tenants.Service is exported by the imported tenants
// module; *slog.Logger comes from the global logger module.
func NewService(repo Repository, ts *tenants.Service, log *slog.Logger) *Service {
	return &Service{repo: repo, tenants: ts, log: log.With("module", "projects")}
}
```

## Controllers, parameters and DTOs

```ts
export class ListProjectsQuery {
  @IsOptional() @Type(() => Number) @IsInt() @Min(1) page = 1;
  @IsOptional() @Type(() => Number) @IsInt() @Min(1) @Max(100) limit = 20;
  @IsOptional() @MaxLength(64) q?: string;
  @IsOptional() @IsIn(['active', 'archived']) status?: string;
}

@Controller('projects')
@UseGuards(JwtGuard, TenantMemberGuard, RolesGuard)
@UseFilters(DomainErrorFilter)
export class ProjectsController {
  @Get()
  list(@Query() q: ListProjectsQuery, @Headers('x-tenant-id') tenant: string) { ... }

  @Delete(':id')
  @Roles('admin')
  @HttpCode(204)
  remove(@Param('id', ParseIntPipe) id: number) { ... }
}
```

<!-- snippet: examples/saas/internal/projects/model.go#inputs -->
```go
// ListInput is the pagination query DTO. Missing or empty values take the
// default; out of range values fail validation with 400.
type ListInput struct {
	tenants.TenantHeader
	Page   int    `query:"page" default:"1" validate:"min=1" doc:"1-based page number"`
	Limit  int    `query:"limit" default:"20" validate:"min=1,max=100" doc:"page size"`
	Search string `query:"q" validate:"omitempty,max=64" doc:"case-insensitive name filter"`
	Status string `query:"status" validate:"omitempty,oneof=active archived"`
}

// IDInput selects one project.
type IDInput struct {
	tenants.TenantHeader
	ID int `path:"id" validate:"min=1"`
}

// CreateInput is the body of POST /projects.
type CreateInput struct {
	tenants.TenantHeader
	Name        string `json:"name" validate:"required,min=3,max=64" example:"Website relaunch"`
	Description string `json:"description" validate:"max=500"`
}

// UpdateInput is the body of PATCH /projects/:id. Nil pointers mean
// "leave unchanged".
type UpdateInput struct {
	tenants.TenantHeader
	ID          int     `path:"id" validate:"min=1"`
	Name        *string `json:"name" validate:"omitempty,min=3,max=64"`
	Description *string `json:"description" validate:"omitempty,max=500"`
	Status      *string `json:"status" validate:"omitempty,oneof=active archived"`
}
```

<!-- snippet: examples/saas/internal/projects/controller.go#routes -->
```go
// Routes implements gonest.Controller. Options passed to r.Use apply to every
// route below it (controller-level decorators in NestJS).
func (c *ProjectsController) Routes(r gonest.Router) {
	r.Prefix("/projects")
	r.Use(
		gonest.Tags("projects"),
		gonest.Security("bearer"),
		// Guards run in order: authenticate, check tenant membership, check roles.
		gonest.UseGuards(c.jwt, tenants.MemberGuard, c.roles),
		gonest.UseFilters(DomainErrorFilter),
	)

	r.GET("", gonest.Handle(c.List), gonest.Summary("List projects"))
	r.POST("", gonest.Handle(c.Create),
		gonest.Summary("Create a project"),
		gonest.UsePipes(TrimPipe),
		gonest.Responds(http.StatusConflict, "Name already used", gonest.ErrorBody{}),
		gonest.Responds(http.StatusUnprocessableEntity, "Plan quota exceeded", gonest.ErrorBody{}))
	r.GET("/:id", gonest.Handle(c.FindOne),
		gonest.Summary("Get a project"),
		gonest.Responds(http.StatusNotFound, "Not found", gonest.ErrorBody{}))
	r.PATCH("/:id", gonest.Handle(c.Update),
		gonest.Summary("Update a project"),
		gonest.Responds(http.StatusNotFound, "Not found", gonest.ErrorBody{}),
		gonest.Responds(http.StatusConflict, "Name already used", gonest.ErrorBody{}),
		gonest.Responds(http.StatusUnprocessableEntity, "Archived project", gonest.ErrorBody{}))
	r.DELETE("/:id", gonest.Handle(c.Delete),
		gonest.Summary("Delete a project (admins only)"),
		auth.Roles(auth.RoleAdmin),
		gonest.Responds(http.StatusNotFound, "Not found", gonest.ErrorBody{}))
}
```

## Guards and custom decorators

```ts
export const Roles = (...roles: string[]) => SetMetadata('roles', roles);

@Injectable()
export class RolesGuard implements CanActivate {
  constructor(private reflector: Reflector) {}
  canActivate(ctx: ExecutionContext): boolean {
    const roles = this.reflector.getAllAndOverride<string[]>('roles', [ctx.getHandler(), ctx.getClass()]);
    if (!roles) return true;
    const user = ctx.switchToHttp().getRequest().user;
    return roles.some((r) => user.roles.includes(r));
  }
}

export const CurrentUser = createParamDecorator((_, ctx: ExecutionContext) =>
  ctx.switchToHttp().getRequest().user);
```

<!-- snippet: examples/saas/internal/auth/guards.go#roles -->
```go
// Roles requires the authenticated user to have at least one of roles
// (NestJS @Roles() built on SetMetadata). Use it on a route or, through
// Router.Use, on a whole controller, together with RolesGuard.
func Roles(roles ...string) gonest.RouteOption {
	return gonest.SetMetadata(rolesKey, roles)
}

// RolesGuard enforces Roles metadata. Routes without metadata are allowed.
// It must run after JWTGuard.
type RolesGuard struct{}

// NewRolesGuard is the provider constructor.
func NewRolesGuard() *RolesGuard { return &RolesGuard{} }

// CanActivate implements gonest.Guard. Route metadata wins over controller
// metadata (Ctx.Route().Metadata looks at the route first).
func (RolesGuard) CanActivate(ctx *gonest.Ctx) (bool, error) {
	v, ok := ctx.Route().Metadata(rolesKey)
	if !ok {
		return true, nil
	}
	user, ok := CurrentUser(ctx)
	if !ok {
		return false, gonest.Unauthorized("not authenticated")
	}
	for _, r := range v.([]string) {
		if user.HasRole(r) {
			return true, nil
		}
	}
	return false, nil // 403 Forbidden resource
}
```

<!-- snippet: examples/saas/internal/auth/guards.go#current-user -->
```go
// CurrentUser returns the user authenticated by JWTGuard for this request
// (the NestJS @CurrentUser() custom decorator).
func CurrentUser(ctx *gonest.Ctx) (*User, bool) {
	return gonest.CtxValue[*User](ctx, UserKey)
}
```

## Interceptors

```ts
@Injectable()
export class TimingInterceptor implements NestInterceptor {
  intercept(ctx: ExecutionContext, next: CallHandler): Observable<any> {
    const start = Date.now();
    return next.handle().pipe(tap(() => {
      ctx.switchToHttp().getResponse().setHeader('X-Response-Time', `${Date.now() - start}ms`);
    }));
  }
}
// providers: [{ provide: APP_INTERCEPTOR, useClass: TimingInterceptor }]
```

<!-- snippet: examples/saas/internal/platform/platform.go#timing-interceptor -->
```go
// TimingHeader is set on every response produced through the pipeline.
const TimingHeader = "X-Response-Time"

// TimingInterceptor measures the time spent in the rest of the pipeline
// (inner interceptors, binding, pipes and the handler). Registered with
// gonest.AppInterceptor, it is a global interceptor built by the container,
// so its own dependencies are injected.
type TimingInterceptor struct {
	slowAfter time.Duration
}

// NewTimingInterceptor is the provider constructor. The logger parameter is
// not needed at construction; it shows that enhancers are ordinary providers.
func NewTimingInterceptor(l *slog.Logger) *TimingInterceptor {
	l.Debug("timing interceptor ready")
	return &TimingInterceptor{slowAfter: 250 * time.Millisecond}
}

// Intercept implements gonest.Interceptor. The code after next() runs before
// the response is written, so the header still reaches the client, also when
// next returns an error that an exception filter renders.
func (t *TimingInterceptor) Intercept(ctx *gonest.Ctx, next gonest.Next) (any, error) {
	start := time.Now()
	out, err := next()
	d := time.Since(start)
	ctx.Writer().Header().Set(TimingHeader, d.String())
	if d > t.slowAfter {
		// logger.FromCtx returns the request logger (with request_id) set by
		// logger.RequestLogger, or Ctx.Logger() when that middleware is absent.
		logger.FromCtx(ctx).Warn("slow handler", "route", ctx.Route().Path, "duration", d)
	}
	return out, err
}
```

## Pipes

```ts
@Injectable()
export class TrimPipe implements PipeTransform {
  transform(value: CreateProjectDto) {
    return { ...value, name: value.name.trim() };
  }
}
```

<!-- snippet: examples/saas/internal/projects/controller.go#pipe -->
```go
// TrimPipe trims the name and description of a CreateInput. Pipes run after
// binding and validation and must return a value of the same type
// (meta.Type), or an error.
var TrimPipe = gonest.PipeFunc(func(_ *gonest.Ctx, v any, _ gonest.ArgMeta) (any, error) {
	in, ok := v.(CreateInput)
	if !ok {
		return v, nil
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if len(in.Name) < 3 {
		return nil, gonest.BadRequest("name must have at least 3 non-blank characters")
	}
	return in, nil
})
```

Validation is not a pipe you add: binding and `validate` tags always run
before your pipes.

## Exception filters

```ts
@Catch(DomainError)
export class DomainErrorFilter implements ExceptionFilter {
  catch(err: DomainError, host: ArgumentsHost) {
    host.switchToHttp().getResponse().status(422).json({ statusCode: 422, message: err.message });
  }
}
```

<!-- snippet: examples/saas/internal/projects/errors.go#domain-filter -->
```go
// DomainError is a business rule violation. Services return it without
// knowing about HTTP; DomainErrorFilter turns it into a 422 response.
type DomainError struct {
	Code    string
	Message string
}

// Error implements error.
func (e *DomainError) Error() string { return e.Code + ": " + e.Message }

// DomainErrorFilter is an exception filter (NestJS @Catch(DomainError)).
// It handles *DomainError anywhere in the error chain and lets every other
// error fall through to the next filter (and finally the default one).
var DomainErrorFilter = gonest.ExceptionFilterFunc(func(ctx *gonest.Ctx, err error) bool {
	var de *DomainError
	if !errors.As(err, &de) {
		return false
	}
	body := gonest.UnprocessableEntity(de.Message, map[string]string{"code": de.Code}).Body()
	ctx.Gin().JSON(http.StatusUnprocessableEntity, body)
	return true
})
```

## Middleware

```ts
export class AppModule implements NestModule {
  configure(consumer: MiddlewareConsumer) {
    consumer.apply(LoggerMiddleware).exclude('health').forRoutes('*');
    consumer.apply(TenantMiddleware)
      .exclude('health', 'auth/(.*)', 'openapi.json', 'docs/(.*)')
      .forRoutes('*');
  }
}
```

<!-- snippet: examples/saas/internal/app/app.go#root-configure -->
```go
// configure is the root module's Configure (NestJS configure(consumer)): its
// parameters after the consumer are injected. Rules apply to the routes of
// the whole application; paths are relative to the global prefix and a
// trailing * matches the path itself and everything below it.
func configure(m *gonest.MiddlewareConsumer, l *slog.Logger, tenantSvc *tenants.Service) {
	// Request logging for every route except the health probe.
	m.Apply(logger.RequestLogger(l)).
		Exclude(gonest.ForPath("/health")).
		ForAllRoutes()
	// Tenant resolution is deny by default: every route requires a valid
	// X-Tenant-ID except the tenant-independent ones listed here, so a new
	// controller is tenant-scoped without extra wiring.
	m.Apply(tenantSvc.Middleware()).
		Exclude(
			gonest.ForPath("/health"),
			gonest.ForPath("/auth/*"),
			gonest.ForPath(openapi.DefaultJSONPath),
			gonest.ForPath(openapi.DefaultUIPath+"/*"),
		).
		ForAllRoutes()
}
```

## Lifecycle hooks

```ts
@Injectable()
export class ProjectsService implements OnModuleInit, OnApplicationShutdown {
  async onModuleInit() { await this.seed(); }
  async onApplicationShutdown() { this.logger.log('stopped'); }
}
```

<!-- snippet: examples/saas/internal/projects/service.go#lifecycle -->
```go
// OnModuleInit seeds demo data once the module's providers are built
// (NestJS OnModuleInit).
func (s *Service) OnModuleInit(ctx context.Context) error {
	seed := map[string][]string{
		"acme":   {"Website relaunch", "Mobile app", "Data warehouse"},
		"globex": {"Moonshot"},
	}
	for _, id := range s.tenants.IDs() {
		for _, name := range seed[id] {
			if _, err := s.Create(ctx, id, name, ""); err != nil {
				return fmt.Errorf("seed %s/%s: %w", id, name, err)
			}
		}
	}
	s.log.InfoContext(ctx, "seeded projects", "tenants", len(seed))
	return nil
}

// OnApplicationShutdown runs last during graceful shutdown; a real service
// would flush buffers or close connections here.
func (s *Service) OnApplicationShutdown(ctx context.Context) error {
	s.log.InfoContext(ctx, "projects service stopped")
	return nil
}
```

## Testing

```ts
const moduleRef = await Test.createTestingModule({ imports: [AppModule] })
  .overrideProvider(Clock).useValue(fakeClock)
  .compile();
const app = moduleRef.createNestApplication();
await app.init();
await request(app.getHttpServer()).get('/auth/me').set('Authorization', `Bearer ${tok}`).expect(200);
```

<!-- snippet: examples/saas/internal/app/wiring_test.go#testing-override -->
```go
// TestTokenExpiry replaces the auth module's Clock provider, so the test
// controls time without sleeping.
func TestTokenExpiry(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	a := gonesttest.New(t, app.NewModule(env(map[string]string{"SAAS_TOKEN_TTL": "5m"}))).
		OverrideProvider(gonest.Value[auth.Clock](clock)).
		Compile()

	tok := login(t, a, "alice@acme.test")
	a.Request("GET", "/auth/me").WithBearer(tok).Do().ExpectStatus(http.StatusOK)

	mu.Lock()
	now = now.Add(6 * time.Minute)
	mu.Unlock()
	a.Request("GET", "/auth/me").WithBearer(tok).Do().
		ExpectStatus(http.StatusUnauthorized).
		ExpectErrorMessage("token expired")
}
```

## What is different

- **No decorators, no reflection metadata.** Everything is a value; "go to
  definition" and the compiler work on all wiring.
- **Errors are returned**, not thrown. A handler returns `(Out, error)`;
  startup returns an error from `gonest.New` instead of crashing later.
- **Wiring is checked at startup.** Missing providers, invisible exports,
  import cycles, bad input tags, duplicate routes and undefined OpenAPI
  security schemes all fail `gonest.New`.
- **One input struct per handler** instead of parameter decorators;
  validation is always on.
- **Interceptors are synchronous** (`next()` returns the result); there are
  no Observables. Use goroutines and `ctx` (a `context.Context`) for
  concurrency and cancellation.
- **No request-scoped providers** in v0.1. Per-request data travels in
  request values (`ctx.Set`, `gonest.CtxValue`), as the current user and
  tenant do in the example.
- **Middleware is plain Gin** (`gin.HandlerFunc`), so the Gin ecosystem
  (CORS, gzip, rate limiting) plugs into `Module.Configure` directly.
