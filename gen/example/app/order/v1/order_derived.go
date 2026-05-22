package orderv1

// AfterOn computes derived fields from the items collection.
// Called once after full event replay in Load, once after all new events in
// Apply (materialization), and inside Builder.Snapshot when a snapshot is
// actually emitted. Not called per-event — safe to iterate collections here.
func (o *Order) AfterOn() {
	o.ItemCount = int32(len(o.Items))
	var total int64
	for _, item := range o.Items {
		total += item.GetPriceCents() * int64(item.GetQuantity())
	}
	o.TotalCents = total
}
