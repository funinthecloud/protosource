package orderv1

// AfterOn computes derived fields from the items collection.
// Called automatically after each On() during event replay and materialization.
func (o *Order) AfterOn() {
	o.ItemCount = int32(len(o.Items))
	var total int64
	for _, item := range o.Items {
		total += item.GetPriceCents() * int64(item.GetQuantity())
	}
	o.TotalCents = total
}
