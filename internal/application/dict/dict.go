package dict

import (
	"context"
	"log/slog"

	dictv1 "glex/api/dict/v1"
	dictentity "glex/internal/domain/dict/entity"
	dictservice "glex/internal/domain/dict/service"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertDictType maps a dictentity.DictType DO to the proto DictType DTO.
func convertDictType(t *dictentity.DictType) *dictv1.DictType {
	return &dictv1.DictType{
		DictTypeId: t.ID,
		DictName:   t.DictName,
		DictType:   t.DictType,
		Remark:     t.Remark,
		CreateBy:   t.CreateBy,
		UpdateBy:   t.UpdateBy,
		CreatedAt:  timestamppb.New(t.CreatedAt),
		UpdatedAt:  timestamppb.New(t.UpdatedAt),
	}
}

// convertDictEntry maps a dictentity.DictEntry DO to the proto DictEntry DTO.
func convertDictEntry(e *dictentity.DictEntry) *dictv1.DictEntry {
	return &dictv1.DictEntry{
		EntryId:    e.ID,
		DictType:   e.DictType,
		DictLabel:  e.DictLabel,
		DictValue:  e.DictValue,
		DictSort:   int32(e.DictSort),
		TagStyle:   tagStyleToProto(e.TagStyle),
		IsDefault:  e.IsDefault,
		Remark:     e.Remark,
		CreateBy:   e.CreateBy,
		UpdateBy:   e.UpdateBy,
		CreatedAt:  timestamppb.New(e.CreatedAt),
		UpdatedAt:  timestamppb.New(e.UpdatedAt),
	}
}

// tagStyleToWire enumerates the stored presets (RuoYi list_class) exactly
// once; the inverse is derived, so the two mapping directions cannot drift.
var tagStyleToWire = map[string]dictv1.TagStyle{
	"default": dictv1.TagStyle_TAG_STYLE_DEFAULT,
	"primary": dictv1.TagStyle_TAG_STYLE_PRIMARY,
	"success": dictv1.TagStyle_TAG_STYLE_SUCCESS,
	"warning": dictv1.TagStyle_TAG_STYLE_WARNING,
	"danger":  dictv1.TagStyle_TAG_STYLE_DANGER,
	"info":    dictv1.TagStyle_TAG_STYLE_INFO,
}

var wireToTagStyle = func() map[dictv1.TagStyle]string {
	inverse := make(map[dictv1.TagStyle]string, len(tagStyleToWire))
	for stored, wire := range tagStyleToWire {
		inverse[wire] = stored
	}
	return inverse
}()

// tagStyleToProto maps the stored preset (RuoYi list_class) to the wire enum;
// an unknown preset normalizes to the plain one.
func tagStyleToProto(style string) dictv1.TagStyle {
	if wire, ok := tagStyleToWire[style]; ok {
		return wire
	}
	return dictv1.TagStyle_TAG_STYLE_DEFAULT
}

// tagStyleFromProto maps the wire enum to the stored preset; the unspecified
// value and anything unknown normalize to the plain preset.
func tagStyleFromProto(style dictv1.TagStyle) string {
	if stored, ok := wireToTagStyle[style]; ok {
		return stored
	}
	return "default"
}

// DictApplication implements the dictionary management service. It owns the
// module's transaction boundary; all repository access goes through the dict
// domain service, where the intra-domain rules live too (ADR 0005 — this
// module has no cross-domain orchestration). Authentication and the
// operation's Functional Permission policy are enforced by the registered
// HTTP middleware.
type DictApplication struct {
	dictv1.UnimplementedDictServiceServer

	svc    dictservice.Dict
	tx     Transaction
	logger *slog.Logger
}

// NewDictApplication creates a DictApplication. svc is the dict domain
// service — the module's only repository holder; tx wraps the writes.
func NewDictApplication(svc dictservice.Dict, tx Transaction, logger *slog.Logger) *DictApplication {
	return &DictApplication{
		svc:    svc,
		tx:     tx,
		logger: logger.With(slog.String("component", "application.dict")),
	}
}

// CreateDictType creates a dict type. dict_name must be non-blank and
// dict_type must match ^[a-z][a-z0-9_]*$ and be unique among active types.
func (s *DictApplication) CreateDictType(ctx context.Context, req *dictv1.CreateDictTypeRequest) (*dictv1.DictType, error) {
	in := req.GetDictType()
	t := &dictentity.DictType{
		DictName: in.GetDictName(),
		DictType: in.GetDictType(),
		Remark:   in.GetRemark(),
	}
	actor := auditActor(ctx)
	t.CreateBy, t.UpdateBy = actor, actor
	var created *dictentity.DictType
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.svc.CreateType(ctx, t)
		return err
	}); err != nil {
		return nil, err
	}
	return convertDictType(created), nil
}

// GetDictType retrieves a dict type by id.
func (s *DictApplication) GetDictType(ctx context.Context, req *dictv1.GetDictTypeRequest) (*dictv1.DictType, error) {
	t, err := s.svc.GetType(ctx, req.GetDictTypeId())
	if err != nil {
		return nil, err
	}
	return convertDictType(t), nil
}

// ListDictTypes lists dict types with AIP-160 filter, AIP-132 order, and
// page-token pagination.
func (s *DictApplication) ListDictTypes(ctx context.Context, req *dictv1.ListDictTypesRequest) (*dictv1.DictTypeSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("dict_name", filtering.TypeString),
		filtering.DeclareIdent("dict_type", filtering.TypeString),
	)
	if err != nil {
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		// Default order: id ascending — RuoYi's dict-type listing order.
		orderBy.Fields = []ordering.Field{{Path: "id"}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	types, err := s.svc.ListTypes(ctx, filter, orderBy, int(req.GetPageSize()), int(pageToken.Offset))
	if err != nil {
		return nil, err
	}
	set := &dictv1.DictTypeSet{DictTypes: make([]*dictv1.DictType, 0, len(types))}
	for _, t := range types {
		set.DictTypes = append(set.DictTypes, convertDictType(t))
	}
	if len(types) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// UpdateDictType partially updates a dict type via field-mask. It loads the
// current state as a DTO, applies the mask onto it, then persists. The key is
// immutable (ADR 0006): the entity sent to the service never carries the
// client's dict_type, and the service re-reads the stored key regardless.
func (s *DictApplication) UpdateDictType(ctx context.Context, req *dictv1.UpdateDictTypeRequest) (*dictv1.DictType, error) {
	current, err := s.GetDictType(ctx, &dictv1.GetDictTypeRequest{DictTypeId: req.GetDictType().GetDictTypeId()})
	if err != nil {
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetDictType())
	t := &dictentity.DictType{
		ID:       current.GetDictTypeId(),
		DictName: current.GetDictName(),
		Remark:   current.GetRemark(),
		UpdateBy: auditActor(ctx),
	}
	var updated *dictentity.DictType
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		updated, err = s.svc.UpdateType(ctx, t)
		return err
	}); err != nil {
		return nil, err
	}
	return convertDictType(updated), nil
}

// DeleteDictType soft-deletes a dict type by id; refused while entries are
// still active under its key (RuoYi: 已分配，不能删除).
func (s *DictApplication) DeleteDictType(ctx context.Context, req *dictv1.DeleteDictTypeRequest) (*emptypb.Empty, error) {
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		return s.svc.DeleteType(ctx, req.GetDictTypeId(), auditActor(ctx))
	}); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// CreateDictEntry creates an entry under an active type. dict_label /
// dict_value must be non-blank and (dict_type, dict_value) unique among
// active entries.
func (s *DictApplication) CreateDictEntry(ctx context.Context, req *dictv1.CreateDictEntryRequest) (*dictv1.DictEntry, error) {
	in := req.GetEntry()
	e := &dictentity.DictEntry{
		DictType:  in.GetDictType(),
		DictLabel: in.GetDictLabel(),
		DictValue: in.GetDictValue(),
		DictSort:  int(in.GetDictSort()),
		TagStyle:  tagStyleFromProto(in.GetTagStyle()),
		IsDefault: in.GetIsDefault(),
		Remark:    in.GetRemark(),
	}
	actor := auditActor(ctx)
	e.CreateBy, e.UpdateBy = actor, actor
	var created *dictentity.DictEntry
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.svc.CreateEntry(ctx, e)
		return err
	}); err != nil {
		return nil, err
	}
	return convertDictEntry(created), nil
}

// GetDictEntry retrieves an entry by id.
func (s *DictApplication) GetDictEntry(ctx context.Context, req *dictv1.GetDictEntryRequest) (*dictv1.DictEntry, error) {
	e, err := s.svc.GetEntry(ctx, req.GetEntryId())
	if err != nil {
		return nil, err
	}
	return convertDictEntry(e), nil
}

// ListDictEntries lists entries with AIP-160 filter, AIP-132 order, and
// page-token pagination. Filtering on dict_type is the drawer's "entries of
// this type" read.
func (s *DictApplication) ListDictEntries(ctx context.Context, req *dictv1.ListDictEntriesRequest) (*dictv1.DictEntrySet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("dict_type", filtering.TypeString),
		filtering.DeclareIdent("dict_label", filtering.TypeString),
		filtering.DeclareIdent("dict_value", filtering.TypeString),
	)
	if err != nil {
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		// Default order: display order, then id as a stable tie-break —
		// RuoYi's dict-data listing order.
		orderBy.Fields = []ordering.Field{{Path: "dict_sort"}, {Path: "id"}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	entries, err := s.svc.ListEntries(ctx, filter, orderBy, int(req.GetPageSize()), int(pageToken.Offset))
	if err != nil {
		return nil, err
	}
	set := &dictv1.DictEntrySet{Entries: make([]*dictv1.DictEntry, 0, len(entries))}
	for _, e := range entries {
		set.Entries = append(set.Entries, convertDictEntry(e))
	}
	if len(entries) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// UpdateDictEntry partially updates an entry via field-mask. The owning type
// key is immutable (ADR 0006): the entity sent to the service never carries
// the client's dict_type, and the service re-reads the stored key regardless.
func (s *DictApplication) UpdateDictEntry(ctx context.Context, req *dictv1.UpdateDictEntryRequest) (*dictv1.DictEntry, error) {
	current, err := s.GetDictEntry(ctx, &dictv1.GetDictEntryRequest{EntryId: req.GetEntry().GetEntryId()})
	if err != nil {
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetEntry())
	e := &dictentity.DictEntry{
		ID:        current.GetEntryId(),
		DictLabel: current.GetDictLabel(),
		DictValue: current.GetDictValue(),
		DictSort:  int(current.GetDictSort()),
		TagStyle:  tagStyleFromProto(current.GetTagStyle()),
		IsDefault: current.GetIsDefault(),
		Remark:    current.GetRemark(),
		UpdateBy:  auditActor(ctx),
	}
	var updated *dictentity.DictEntry
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		updated, err = s.svc.UpdateEntry(ctx, e)
		return err
	}); err != nil {
		return nil, err
	}
	return convertDictEntry(updated), nil
}

// DeleteDictEntry soft-deletes an entry by id.
func (s *DictApplication) DeleteDictEntry(ctx context.Context, req *dictv1.DeleteDictEntryRequest) (*emptypb.Empty, error) {
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		return s.svc.DeleteEntry(ctx, req.GetEntryId(), auditActor(ctx))
	}); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListDictEntriesByType returns one type's active entries as lightweight
// options, ordered by dict_sort then id — the consumption read every page
// uses for selects and status tags. An unknown type key yields an empty
// list; the read needs no transaction (single query).
func (s *DictApplication) ListDictEntriesByType(ctx context.Context, req *dictv1.ListDictEntriesByTypeRequest) (*dictv1.DictEntryOptionSet, error) {
	entries, err := s.svc.OptionsByType(ctx, req.GetDictType())
	if err != nil {
		return nil, err
	}
	set := &dictv1.DictEntryOptionSet{Options: make([]*dictv1.DictEntryOption, 0, len(entries))}
	for _, e := range entries {
		set.Options = append(set.Options, &dictv1.DictEntryOption{
			Value:     e.DictValue,
			Label:     e.DictLabel,
			TagStyle:  tagStyleToProto(e.TagStyle),
			IsDefault: e.IsDefault,
		})
	}
	return set, nil
}
