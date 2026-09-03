package access

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	accessv1 "glex/api/access/v1"
	"glex/internal/bizpkg"
	"glex/internal/crossdomain/authn"
	accessentity "glex/internal/domain/access/entity"
	accessservice "glex/internal/domain/access/service"
	authndomain "glex/internal/domain/authn"
	authnservice "glex/internal/domain/authn/service"
	"glex/pkg/logx"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertMenu maps a accessentity.Menu DO to the proto Menu DTO, recursing into children.
// children is read-only: it is populated here for ListMenus and never read on
// the write path.
func convertMenu(m *accessentity.Menu) *accessv1.Menu {
	out := &accessv1.Menu{
		MenuId:     m.ID,
		MenuName:   m.MenuName,
		ParentId:   m.ParentID,
		OrderNum:   int32(m.OrderNum),
		Path:       m.Path,
		Component:  m.Component,
		QueryParam: m.QueryParam,
		IsFrame:    m.IsFrame,
		IsCache:    m.IsCache,
		MenuType:   accessv1.MenuType(m.MenuType),
		Visible:    m.Visible,
		Status:     m.Status,
		Perms:      m.Perms,
		Icon:       m.Icon,
		ActiveMenu: m.ActiveMenu,
		Ext:        m.Ext,
		CreateBy:   m.CreateBy,
		UpdateBy:   m.UpdateBy,
		CreatedAt:  timestamppb.New(m.CreatedAt),
		UpdatedAt:  timestamppb.New(m.UpdatedAt),
	}
	for _, c := range m.Children {
		out.Children = append(out.Children, convertMenu(c))
	}
	return out
}

func routeName(path string, menuID int64, menuFrame bool) string {
	if menuFrame {
		return fmt.Sprintf("%d", menuID)
	}
	return capitalizedRouteName(path, menuID)
}

func capitalizedRouteName(path string, menuID int64) string {
	path = strings.Trim(path, "/")
	runes := []rune(path)
	if len(runes) == 0 {
		return fmt.Sprintf("Menu%d", menuID)
	}
	runes[0] = unicode.ToUpper(runes[0])
	return fmt.Sprintf("%s%d", string(runes), menuID)
}

func isMenuFrame(m *accessentity.Menu) bool {
	return m.ParentID == 0 && m.MenuType == accessentity.MenuTypeMenu && m.IsFrame == 0
}

func isInnerLink(m *accessentity.Menu) bool {
	path := strings.ToLower(m.Path)
	return m.IsFrame == 0 && (strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://"))
}

func routePath(m *accessentity.Menu) string {
	path := m.Path
	if m.ParentID != 0 && isInnerLink(m) {
		path = innerLinkPath(path)
	}
	if m.ParentID == 0 && m.MenuType == accessentity.MenuTypeDirectory && m.IsFrame == 0 {
		return "/" + strings.TrimPrefix(path, "/")
	}
	if isMenuFrame(m) {
		return "/"
	}
	return path
}

func routeComponent(m *accessentity.Menu) string {
	if m.Component != "" && !isMenuFrame(m) {
		return m.Component
	}
	if m.Component == "" && m.ParentID != 0 && isInnerLink(m) {
		return "InnerLink"
	}
	if m.Component == "" && m.ParentID != 0 && m.MenuType == accessentity.MenuTypeDirectory {
		return "ParentView"
	}
	return "Layout"
}

func innerLinkPath(path string) string {
	return strings.NewReplacer(
		"https://", "",
		"http://", "",
		"www.", "",
		".", "/",
		":", "/",
	).Replace(path)
}

// MenuApplication implements the system-menu CRUD + tree service. It owns the
// module's cross-domain orchestration: the transaction boundary and session
// revocation through the authn crossdomain contract. All repository access
// goes through the menu domain service; intra-domain rules (type validation,
// sibling-name uniqueness, tree assembly, route selection) live there too
// (ADR 0005). Authentication and the operation's Functional Permission policy
// are enforced by the registered HTTP middleware.
type MenuApplication struct {
	accessv1.UnimplementedMenuServiceServer

	svc     accessservice.Menu
	tx      Transaction
	revoker authn.SessionRevoker
	logger  *slog.Logger
}

// NewMenuApplication creates a MenuApplication. svc is the menu domain
// service — the module's only repository holder; tx wraps security writes
// together with their session revocation; revoker performs the revocation
// through the authn crossdomain contract.
func NewMenuApplication(svc accessservice.Menu, tx Transaction, revoker authn.SessionRevoker, logger *slog.Logger) *MenuApplication {
	return &MenuApplication{
		svc:     svc,
		tx:      tx,
		revoker: revoker,
		logger:  logger.With(slog.String("component", "application.access.menu")),
	}
}

// CreateMenu creates a menu: it validates the type and sibling-name
// uniqueness, then persists. Uniqueness is defense-in-depth at the domain
// layer; there is no DB unique constraint on (parent_id, menu_name).
func (s *MenuApplication) CreateMenu(ctx context.Context, req *accessv1.CreateMenuRequest) (*accessv1.Menu, error) {
	in := req.GetMenu()
	actor := bizpkg.AuditActor(ctx)
	m := &accessentity.Menu{
		MenuName:   in.GetMenuName(),
		ParentID:   in.GetParentId(),
		OrderNum:   int(in.GetOrderNum()),
		Path:       in.GetPath(),
		Component:  in.GetComponent(),
		QueryParam: in.GetQueryParam(),
		IsFrame:    in.GetIsFrame(),
		IsCache:    in.GetIsCache(),
		MenuType:   int32(in.GetMenuType()),
		Visible:    in.GetVisible(),
		Status:     in.GetStatus(),
		Perms:      in.GetPerms(),
		Icon:       in.GetIcon(),
		ActiveMenu: in.GetActiveMenu(),
		Ext:        in.GetExt(),
		CreateBy:   actor,
		UpdateBy:   actor,
	}
	var created *accessentity.Menu
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.svc.Create(ctx, m)
		return err
	}); err != nil {
		logx.Propagate(ctx, s.logger, "access.create_menu", err)
		return nil, err
	}
	return convertMenu(created), nil
}

// GetMenu retrieves a menu by id.
func (s *MenuApplication) GetMenu(ctx context.Context, req *accessv1.GetMenuRequest) (*accessv1.Menu, error) {
	m, err := s.svc.Get(ctx, req.GetMenuId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.get_menu", err)
		return nil, err
	}
	return convertMenu(m), nil
}

// ListMenus returns the full menu tree with AIP-160 filter and AIP-132 order.
// Not paginated (menus are small and tree-shaped).
func (s *MenuApplication) ListMenus(ctx context.Context, req *accessv1.ListMenusRequest) (*accessv1.MenuTree, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("menu_name", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
		filtering.DeclareIdent("visible", filtering.TypeInt),
		filtering.DeclareIdent("menu_type", filtering.TypeInt),
		filtering.DeclareIdent("parent_id", filtering.TypeInt),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_menus", err)
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_menus", err)
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_menus", err)
		return nil, err
	}
	menus, err := s.svc.List(ctx, filter, orderBy)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.list_menus", err)
		return nil, err
	}
	roots := accessservice.BuildMenuTree(menus)
	tree := &accessv1.MenuTree{Menus: make([]*accessv1.Menu, 0, len(roots))}
	for _, m := range roots {
		tree.Menus = append(tree.Menus, convertMenu(m))
	}
	return tree, nil
}

// GetRouters returns the current System User's freshly resolved route tree.
func (s *MenuApplication) GetRouters(ctx context.Context, _ *emptypb.Empty) (*accessv1.RouterTree, error) {
	principal, ok := authnservice.PrincipalFromContext(ctx)
	if !ok {
		err := authndomain.ErrCredentialMissing
		logx.Propagate(ctx, s.logger, "access.get_routers", err)
		return nil, err
	}
	routeAccess, err := s.svc.LoadRouteAccess(ctx, principal.SystemUserID())
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.get_routers", err)
		return nil, err
	}
	roots := accessservice.SelectUserRouteTree(routeAccess)
	toMeta := func(m *accessentity.Menu) *accessv1.RouterMeta {
		link := ""
		if strings.HasPrefix(strings.ToLower(m.Path), "http://") || strings.HasPrefix(strings.ToLower(m.Path), "https://") {
			link = m.Path
		}
		return &accessv1.RouterMeta{
			Title:      m.MenuName,
			Icon:       m.Icon,
			NoCache:    m.IsCache == 0,
			Link:       link,
			ActiveMenu: m.ActiveMenu,
		}
	}
	var toRouter func(*accessentity.Menu) *accessv1.Router
	toRouter = func(m *accessentity.Menu) *accessv1.Router {
		menuFrame := isMenuFrame(m)
		path := routePath(m)
		router := &accessv1.Router{
			Name:      routeName(path, m.ID, menuFrame),
			Path:      path,
			Hidden:    m.Visible == 1,
			Component: routeComponent(m),
			Query:     m.QueryParam,
			Meta:      toMeta(m),
		}
		if len(m.Children) > 0 && m.MenuType == accessentity.MenuTypeDirectory {
			router.AlwaysShow = true
			router.Redirect = "noRedirect"
			router.Children = make([]*accessv1.Router, 0, len(m.Children))
			for _, child := range m.Children {
				router.Children = append(router.Children, toRouter(child))
			}
		} else if menuFrame {
			router.Meta = nil
			router.Children = []*accessv1.Router{{
				Name:      capitalizedRouteName(m.Path, m.ID),
				Path:      m.Path,
				Component: m.Component,
				Query:     m.QueryParam,
				Meta:      toMeta(m),
			}}
		} else if m.ParentID == 0 && isInnerLink(m) {
			router.Path = "/"
			router.Meta = &accessv1.RouterMeta{Title: m.MenuName, Icon: m.Icon}
			innerPath := innerLinkPath(m.Path)
			router.Children = []*accessv1.Router{{
				Name:      capitalizedRouteName(innerPath, m.ID),
				Path:      innerPath,
				Component: "InnerLink",
				Meta:      toMeta(m),
			}}
		}
		return router
	}
	tree := &accessv1.RouterTree{Routers: make([]*accessv1.Router, 0, len(roots))}
	for _, root := range roots {
		tree.Routers = append(tree.Routers, toRouter(root))
	}
	return tree, nil
}

// UpdateMenu partially updates a menu via field-mask. It rejects a self-parent
// and re-validates the type (so an explicit mask cannot zero it), enforces
// sibling-name uniqueness excluding self, then persists. When the write
// invalidates Authorization Snapshots (perms/status), affected users' Login
// Sessions are revoked inside the same transaction. That failure behavior is
// an implementation detail for this operation rather than a stable business
// guarantee.
func (s *MenuApplication) UpdateMenu(ctx context.Context, req *accessv1.UpdateMenuRequest) (*accessv1.Menu, error) {
	current, err := s.GetMenu(ctx, &accessv1.GetMenuRequest{MenuId: req.GetMenu().GetMenuId()})
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.update_menu", err)
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetMenu())
	m := &accessentity.Menu{
		ID:         current.GetMenuId(),
		MenuName:   current.GetMenuName(),
		ParentID:   current.GetParentId(),
		OrderNum:   int(current.GetOrderNum()),
		Path:       current.GetPath(),
		Component:  current.GetComponent(),
		QueryParam: current.GetQueryParam(),
		IsFrame:    current.GetIsFrame(),
		IsCache:    current.GetIsCache(),
		MenuType:   int32(current.GetMenuType()),
		Visible:    current.GetVisible(),
		Status:     current.GetStatus(),
		Perms:      current.GetPerms(),
		Icon:       current.GetIcon(),
		ActiveMenu: current.GetActiveMenu(),
		Ext:        current.GetExt(),
		UpdateBy:   bizpkg.AuditActor(ctx),
	}
	var updated *accessentity.Menu
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		result, err := s.svc.Update(ctx, m)
		if err != nil {
			return err
		}
		updated = result.Menu
		if result.Menu.AuthorizationChangedFrom(result.Previous) {
			return revokeAllSystemUsers(ctx, s.revoker, result.AffectedUserIDs...)
		}
		return nil
	}); err != nil {
		logx.Propagate(ctx, s.logger, "access.update_menu", err)
		return nil, err
	}
	return convertMenu(updated), nil
}

// DeleteMenu soft-deletes a menu by id; refused while it has children. The
// soft delete removes its role links and revokes affected users' Login
// Sessions in one transaction.
func (s *MenuApplication) DeleteMenu(ctx context.Context, req *accessv1.DeleteMenuRequest) (*emptypb.Empty, error) {
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := s.svc.Delete(ctx, req.GetMenuId(), bizpkg.AuditActor(ctx))
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, s.revoker, affected...)
	}); err != nil {
		logx.Propagate(ctx, s.logger, "access.delete_menu", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
