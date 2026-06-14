package view

type CapacityMaxima struct {
	MaxOnlineUsers            float64
	MaxInboundMsgPerSec       float64
	MaxBackendFanoutMsgPerSec float64
}

const capacityGaugeWidth = 8

func capacityRatePer5Min(rateMax float64) float64 {
	if rateMax <= 0 {
		return 0
	}
	return rateMax * 300
}

func capacityPercent(value, max float64) float64 {
	if max <= 0 {
		return 0
	}
	if value < 0 {
		value = 0
	}
	return value / max * 100
}

func formatCapacityValue(formatted string, value, max float64) string {
	rendered := ValueStyle.Render(formatted)
	if max <= 0 {
		return rendered
	}
	pct := capacityPercent(value, max)
	return rendered +
		LabelStyle.Render(" ") + ProgressBar(pct, capacityGaugeWidth) +
		LabelStyle.Render(" ") + pctColored(pct)
}
