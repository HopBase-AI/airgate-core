package listprice

import "testing"

func TestConsistent(t *testing.T) {
	tests := []struct {
		name           string
		list, fx, base float64
		want           bool
	}{
		{name: "全精度一致", list: 12, fx: 6.8, base: 12.0 / 6.8, want: true},
		{name: "运营四位小数（相对 0.1% 内）", list: 12, fx: 6.8, base: 1.7647, want: true},
		{name: "小价四位小数", list: 0.2, fx: 6.8, base: 0.0294, want: true},
		{name: "基准价明显错配", list: 12, fx: 6.8, base: 2, want: false},
		{name: "两位小数过粗", list: 12, fx: 6.8, base: 1.76, want: false},
		{name: "fx 非法", list: 12, fx: 0, base: 12, want: false},
		{name: "零价一致", list: 0, fx: 6.8, base: 0, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Consistent(tt.list, tt.fx, tt.base); got != tt.want {
				t.Fatalf("Consistent(%g, %g, %g) = %v, want %v", tt.list, tt.fx, tt.base, got, tt.want)
			}
		})
	}
}
