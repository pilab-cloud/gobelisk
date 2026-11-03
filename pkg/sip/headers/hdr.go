package headers

func (h *RecordRouteHeader) cloneFirst() *RecordRouteHeader {
	var newRoute *RecordRouteHeader
	newRoute = &RecordRouteHeader{
		Address: h.Address,
	}
	return newRoute
}
