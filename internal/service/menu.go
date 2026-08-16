package service

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	menuv1 "glex/api/system/menu/v1"
	"glex/internal/biz"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertMenu maps a biz.Menu DO to the proto Menu DTO, recursing into children.
// children is read-only: it is populated here for ListMenus and never read on
// the write path.
func convertMenu(m *biz.Menu) *menuv1.Menu {
	out := &menuv1.Menu{
		MenuId:     m.ID,
		MenuName:   m.MenuName,
		ParentId:   m.ParentID,
		OrderNum:   int32(m.OrderNum),
		Path:       m.Path,
		Component:  m.Component,
		QueryParam: m.QueryParam,
		IsFrame:    m.IsFrame,
		IsCache:    m.IsCache,
		MenuType:   menuv1.MenuType(m.MenuType),
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

func isMenuFrame(m *biz.Menu) bool {
	return m.ParentID == 0 && m.MenuType == biz.MenuTypeMenu && m.IsFrame == 0
}

func isInnerLink(m *biz.Menu) bool {
	path := strings.ToLower(m.Path)
	return m.IsFrame == 0 && (strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://"))
}

func routePath(m *biz.Menu) string {
	path := m.Path
	if m.ParentID != 0 && isInnerLink(m) {
		path = innerLinkPath(path)
	}
	if m.ParentID == 0 && m.MenuType == biz.MenuTypeDirectory && m.IsFrame == 0 {
		return "/" + strings.TrimPrefix(path, "/")
	}
	if isMenuFrame(m) {
		return "/"
	}
	return path
}

func routeComponent(m *biz.Menu) string {
	if m.Component != "" && !isMenuFrame(m) {
		return m.Component
	}
	if m.Component == "" && m.ParentID != 0 && isInnerLink(m) {
		return "InnerLink"
	}
	if m.Component == "" && m.ParentID != 0 && m.MenuType == biz.MenuTypeDirectory {
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

// MenuService implements the system-menu CRUD + tree service.
//
// Authentication and the operation's Functional Permission policy are
// enforced by the registered HTTP middleware; this adapter only translates
// DTOs and delegates business rules.
type MenuService struct {
	menuv1.UnimplementedMenuServiceServer

	uc menuUsecase
}

type menuUsecase interface {
	CreateMenu(context.Context, *biz.Menu) (*biz.Menu, error)
	GetMenu(context.Context, int64) (*biz.Menu, error)
	ListMenuTree(context.Context, ...biz.ListOption) ([]*biz.Menu, error)
	ListUserRouteTree(context.Context, int64) ([]*biz.Menu, error)
	UpdateMenu(context.Context, *biz.Menu) (*biz.Menu, error)
	DeleteMenu(context.Context, int64) error
}

// NewMenuService creates a MenuService.
func NewMenuService(uc *biz.MenuUsecase) *MenuService {
	return newMenuService(uc)
}

func newMenuService(uc menuUsecase) *MenuService {
	return &MenuService{uc: uc}
}

// CreateMenu creates a menu.
func (s *MenuService) CreateMenu(ctx context.Context, req *menuv1.CreateMenuRequest) (*menuv1.Menu, error) {
	in := req.GetMenu()
	created, err := s.uc.CreateMenu(ctx, &biz.Menu{
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
	})
	if err != nil {
		return nil, err
	}
	return convertMenu(created), nil
}

// GetMenu retrieves a menu by id.
func (s *MenuService) GetMenu(ctx context.Context, req *menuv1.GetMenuRequest) (*menuv1.Menu, error) {
	m, err := s.uc.GetMenu(ctx, req.GetMenuId())
	if err != nil {
		return nil, err
	}
	return convertMenu(m), nil
}

// ListMenus returns the full menu tree with AIP-160 filter and AIP-132 order.
// Not paginated (menus are small and tree-shaped).
func (s *MenuService) ListMenus(ctx context.Context, req *menuv1.ListMenusRequest) (*menuv1.MenuTree, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("menu_name", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
		filtering.DeclareIdent("visible", filtering.TypeInt),
		filtering.DeclareIdent("menu_type", filtering.TypeInt),
		filtering.DeclareIdent("parent_id", filtering.TypeInt),
	)
	if err != nil {
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		return nil, err
	}
	roots, err := s.uc.ListMenuTree(ctx,
		biz.ListFilter(filter),
		biz.ListOrderBy(orderBy),
	)
	if err != nil {
		return nil, err
	}
	tree := &menuv1.MenuTree{Menus: make([]*menuv1.Menu, 0, len(roots))}
	for _, m := range roots {
		tree.Menus = append(tree.Menus, convertMenu(m))
	}
	return tree, nil
}

// GetRouters returns the current System User's freshly resolved route tree.
func (s *MenuService) GetRouters(ctx context.Context, _ *emptypb.Empty) (*menuv1.RouterTree, error) {
	principal, ok := biz.PrincipalFromContext(ctx)
	if !ok {
		return nil, biz.ErrCredentialMissing
	}
	roots, err := s.uc.ListUserRouteTree(ctx, principal.SystemUserID())
	if err != nil {
		return nil, err
	}
	toMeta := func(m *biz.Menu) *menuv1.RouterMeta {
		link := ""
		if strings.HasPrefix(strings.ToLower(m.Path), "http://") || strings.HasPrefix(strings.ToLower(m.Path), "https://") {
			link = m.Path
		}
		return &menuv1.RouterMeta{
			Title:      m.MenuName,
			Icon:       m.Icon,
			NoCache:    m.IsCache == 0,
			Link:       link,
			ActiveMenu: m.ActiveMenu,
		}
	}
	var toRouter func(*biz.Menu) *menuv1.Router
	toRouter = func(m *biz.Menu) *menuv1.Router {
		menuFrame := isMenuFrame(m)
		path := routePath(m)
		router := &menuv1.Router{
			Name:      routeName(path, m.ID, menuFrame),
			Path:      path,
			Hidden:    m.Visible == 1,
			Component: routeComponent(m),
			Query:     m.QueryParam,
			Meta:      toMeta(m),
		}
		if len(m.Children) > 0 && m.MenuType == biz.MenuTypeDirectory {
			router.AlwaysShow = true
			router.Redirect = "noRedirect"
			router.Children = make([]*menuv1.Router, 0, len(m.Children))
			for _, child := range m.Children {
				router.Children = append(router.Children, toRouter(child))
			}
		} else if menuFrame {
			router.Meta = nil
			router.Children = []*menuv1.Router{{
				Name:      capitalizedRouteName(m.Path, m.ID),
				Path:      m.Path,
				Component: m.Component,
				Query:     m.QueryParam,
				Meta:      toMeta(m),
			}}
		} else if m.ParentID == 0 && isInnerLink(m) {
			router.Path = "/"
			router.Meta = &menuv1.RouterMeta{Title: m.MenuName, Icon: m.Icon}
			innerPath := innerLinkPath(m.Path)
			router.Children = []*menuv1.Router{{
				Name:      capitalizedRouteName(innerPath, m.ID),
				Path:      innerPath,
				Component: "InnerLink",
				Meta:      toMeta(m),
			}}
		}
		return router
	}
	tree := &menuv1.RouterTree{Routers: make([]*menuv1.Router, 0, len(roots))}
	for _, root := range roots {
		tree.Routers = append(tree.Routers, toRouter(root))
	}
	return tree, nil
}

// UpdateMenu partially updates a menu via field-mask. It loads the current
// state as a DTO, applies the mask onto it, then persists.
func (s *MenuService) UpdateMenu(ctx context.Context, req *menuv1.UpdateMenuRequest) (*menuv1.Menu, error) {
	current, err := s.GetMenu(ctx, &menuv1.GetMenuRequest{MenuId: req.GetMenu().GetMenuId()})
	if err != nil {
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetMenu())
	updated, err := s.uc.UpdateMenu(ctx, &biz.Menu{
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
	})
	if err != nil {
		return nil, err
	}
	return convertMenu(updated), nil
}

// DeleteMenu soft-deletes a menu by id; refused while it has children.
func (s *MenuService) DeleteMenu(ctx context.Context, req *menuv1.DeleteMenuRequest) (*emptypb.Empty, error) {
	if err := s.uc.DeleteMenu(ctx, req.GetMenuId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
