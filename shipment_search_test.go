package logisticssearch

import "testing"

func TestClassifyException(t *testing.T) {
	tests := []struct {
		name       string
		summary    string
		wantHandle bool
		wantReason string
	}{
		{"normal delivery", "shipment S-18; delivered Shanghai; proof of delivery signed", false, ""},
		{"damaged parcel", "shipment S-19; depot reports outer carton damaged", true, "damage review"},
		{"address correction", "shipment S-20; address mismatch at final depot", true, "address review"},
		{"missing signature", "shipment S-21; delivered; signature missing", true, "proof review"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotHandle, gotReason := ClassifyException(test.summary)
			if gotHandle != test.wantHandle || gotReason != test.wantReason {
				t.Fatalf("ClassifyException() = (%v, %q), want (%v, %q)", gotHandle, gotReason, test.wantHandle, test.wantReason)
			}
		})
	}
}
