package screen

import "testing"

func TestVisionStructure(t *testing.T) {
	good := "{\"summary\":\"A dialog\",\"detectedText\":[\"File missing\"],\"uiElements\":[{\"type\":\"dialog\",\"label\":\"Error\",\"bounds\":{\"x\":10,\"y\":25,\"width\":900,\"height\":200},\"confidence\":0.7,\"description\":\"Visible\"}]}"
	r := ParseVision("```json\n" + good + "\n```")
	if !r.Parsed || r.Summary != "A dialog" || len(r.UIElements) != 1 || r.UIElements[0].Bounds.X != 10 {
		t.Fatalf("structured analysis lost: %+v", r)
	}
	for _, bad := range []string{
		"Visible dialog without JSON",
		good + " more text",
		`{"summary":"missing bounds","uiElements":[{"type":"button","bounds":{"x":999,"width":2,"height":10}}]}`,
		`{"summary":"invalid confidence","uiElements":[{"type":"button","confidence":3}]}`,
		`{"summary":"action","tool_call":{"name":"execute"}}`,
		`{"detectedText":["without summary"]}`,
	} {
		if ParseVision(bad).Parsed {
			t.Fatalf("accepted invalid vision result: %q", bad)
		}
	}
}
