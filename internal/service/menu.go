package service

import (
	"context"

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

// MenuService implements the system-menu CRUD + tree service.
//
// No authz here by design: the global auth middleware will be added by the auth
// module, and finer permission checks belong to the RBAC/auth module.
type MenuService struct {
	menuv1.UnimplementedMenuServiceServer

	uc *biz.MenuUsecase
}

// NewMenuService creates a MenuService.
func NewMenuService(uc *biz.MenuUsecase) *MenuService {
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
