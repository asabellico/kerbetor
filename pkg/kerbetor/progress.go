package kerbetor

import (
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

func NewProgressBar(p *mpb.Progress, barName string, totalSize uint64, priority int) *mpb.Bar {
	return p.AddBar(
		int64(totalSize),
		mpb.BarPriority(priority),
		mpb.PrependDecorators(
			decor.Name(barName, decor.WC{C: decor.DidentRight}),
			decor.Percentage(decor.WCSyncSpace),
		),
		mpb.AppendDecorators(
			decor.CountersKibiByte(" %6.1f/%6.1f", decor.WCSyncWidth),
			decor.Name("[ETA: ", decor.WCSyncSpace),
			decor.AverageETA(decor.ET_STYLE_GO),
			decor.AverageSpeed(decor.UnitKiB, ", %.2f]"),
		),
	)
}
