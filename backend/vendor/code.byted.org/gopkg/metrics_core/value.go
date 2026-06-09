package metrics

import (
	"bytes"
	"fmt"
	"math"
	"strconv"

	"code.byted.org/aiops/metrics_codec"
	"code.byted.org/gopkg/metrics_core/logs"
	"code.byted.org/gopkg/metrics_core/utils"
)

var (
	timerSuffixes      = []string{"min", "max", "avg", "sum", "counter", "pct50", "pct90", "pct95", "pct99", "pct999"}
	histoSuffixes      = []string{"sum", "count"}
	deltaSuffixes      = []string{"delta", "rate", "counter"}
	defaultTimerFields = []metrics_codec.Field{
		{"min", 0}, {"max", 0}, {"avg", 0}, {"sum", 0}, {"counter", 0},
		{"pct50", 0}, {"pct90", 0}, {"pct95", 0}, {"pct99", 0}, {"pct999", 0},
	}
	defaultDeltaFields = []metrics_codec.Field{{"delta", 0}, {"rate", 0}, {"counter", 0}}
)

type fields struct {
	fields         []Field
	index          map[string][]int
	allCodecFields []metrics_codec.Field
}

func (fs *fields) String() string {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, f := range fs.fields {
		if i != 0 {
			b.WriteString(", ")
		}
		b.WriteByte('(')
		b.WriteString(f.Name)
		b.WriteByte(',')
		b.WriteString(f.Type.String())
		b.WriteByte(')')
	}
	b.WriteByte(']')
	return b.String()
}

// expandFields parses the fields provided by the users and compute their indices.
// withSuffixAsFieldName is true when the metric type is MultiFieldType, otherwise it is false.
func (fs *fields) expandFields(withSuffixAsFieldName bool, maxFieldCount int) error {
	var err error
	if len(fs.fields) == 0 {
		return nil
	}

	// Check duplicate field names and invalid field names
	seen := make(map[string]bool)
	for _, v := range fs.fields {
		if _, ok := seen[v.Name]; ok {
			return fmt.Errorf("cannot have two fields with the same name: \"%s\"", v.Name)
		}

		if !utils.IsValidString(v.Name) {
			return fmt.Errorf("Invalid Field name: %s, accepted chars: a-zA-Z0-9._-", v.Name)
		}
		seen[v.Name] = true
	}

	// Check number of fields and number of histogram buckets
	fieldCount := 0
	fs.index = make(map[string][]int, len(fs.fields))

	valueIndex := 0
	for i := 0; i < len(fs.fields); i++ {
		if fs.fields[i].Type == TimerType {
			fs.fields[i].meta = metaFields{}
			for _, v := range timerSuffixes {
				if withSuffixAsFieldName {
					fs.fields[i].meta.codecFields = append(fs.fields[i].meta.codecFields, metrics_codec.Field{
						Name:  fmt.Sprintf("%s.%s", fs.fields[i].Name, v),
						VType: 0,
					})
				} else {
					fs.fields[i].meta.codecFields = append(fs.fields[i].meta.codecFields, metrics_codec.Field{
						Name:  v,
						VType: 0,
					})
				}
			}

			fs.index[fs.fields[i].Name] = []int{i, valueIndex}
			valueIndex += len(timerSuffixes)
			fieldCount += len(timerSuffixes)
		} else if fs.fields[i].Type == HistogramType {
			originalBuckets := fs.fields[i].Buckets
			buckets, err := checkBuckets(originalBuckets)
			if err != nil {
				return err
			}
			fs.fields[i].meta = metaFields{}
			fs.fields[i].meta.buckets = buckets

			for _, v := range buckets {
				if !withSuffixAsFieldName {
					fs.fields[i].meta.codecFields = append(fs.fields[i].meta.codecFields, metrics_codec.Field{
						Name:  fmt.Sprintf("hist:b-%s", strconv.FormatFloat(v, 'f', -1, 64)),
						VType: 0,
					})
				} else {
					fs.fields[i].meta.codecFields = append(fs.fields[i].meta.codecFields, metrics_codec.Field{
						Name:  fmt.Sprintf("hist:%s:b-%s", fs.fields[i].Name, strconv.FormatFloat(v, 'f', -1, 64)),
						VType: 0,
					})
				}
			}

			for _, v := range histoSuffixes {
				if !withSuffixAsFieldName && fs.fields[i].Name == defaultHistogramFieldName {
					fs.fields[i].meta.codecFields = append(fs.fields[i].meta.codecFields, metrics_codec.Field{
						Name:  fmt.Sprintf("hist:%s", v),
						VType: 0,
					})
				} else {

					fs.fields[i].meta.codecFields = append(fs.fields[i].meta.codecFields, metrics_codec.Field{
						Name:  fmt.Sprintf("hist:%s:%s", fs.fields[i].Name, v),
						VType: 0,
					})
				}
			}

			fs.index[fs.fields[i].Name] = []int{i, valueIndex}
			valueIndex += len(fs.fields[i].meta.codecFields)
			fieldCount += len(fs.fields[i].meta.codecFields)
		} else if fs.fields[i].Type == MeterType {
			fs.fields[i].Type = CounterType
			fs.fields[i].meta = metaFields{
				nil,
				[]metrics_codec.Field{{Name: fs.fields[i].Name, VType: 0}},
			}

			fs.index[fs.fields[i].Name] = []int{i, valueIndex}
			valueIndex++

			newField := fs.fields[i].Name + ".rate"
			fs.fields = append(fs.fields[:i+1], append([]Field{{Name: newField, Type: RateCounterType}}, fs.fields[i+1:]...)...)
			fieldCount += 2
		} else if fs.fields[i].Type == DeltaCounterType {
			fs.fields[i].meta = metaFields{}
			for _, v := range deltaSuffixes {
				if withSuffixAsFieldName {
					fs.fields[i].meta.codecFields = append(fs.fields[i].meta.codecFields, metrics_codec.Field{
						Name:  fmt.Sprintf("%s.%s", fs.fields[i].Name, v),
						VType: 0,
					})
				} else {
					fs.fields[i].meta.codecFields = append(fs.fields[i].meta.codecFields, metrics_codec.Field{
						Name:  v,
						VType: 0,
					})
				}
			}

			fs.index[fs.fields[i].Name] = []int{i, valueIndex}
			valueIndex += len(fs.fields[i].meta.codecFields)
			fieldCount += len(fs.fields[i].meta.codecFields)
		} else {
			fs.fields[i].meta = metaFields{
				nil,
				[]metrics_codec.Field{{Name: fs.fields[i].Name, VType: 0}},
			}

			fs.index[fs.fields[i].Name] = []int{i, valueIndex}
			valueIndex++
			fieldCount++
		}
	}

	if fieldCount > maxFieldCount {
		return fmt.Errorf("over %d fields: %d", maxFieldCount, fieldCount)
	}

	codecFields := make([]metrics_codec.Field, 0, 10)
	for _, v := range fs.fields {
		codecFields = append(codecFields, v.meta.codecFields...)
	}
	fs.allCodecFields = codecFields
	return err
}

func (fs *fields) getAllCodecFields() []metrics_codec.Field {
	return fs.allCodecFields
}

func (fs *fields) getHistogramFields(name string) []metrics_codec.Field {
	i := fs.indexOf(name, HistogramType)
	if i == -1 {
		logs.Error("didn't found histogram fields for %s", name)
		return nil
	}

	return fs.fields[i].meta.codecFields

}

func (fs *fields) getTimerFields(name string, withSuffix bool) []metrics_codec.Field {
	if !withSuffix {
		return defaultTimerFields
	}

	i := fs.indexOf(name, TimerType)
	if i == -1 {
		return nil
	}

	return fs.fields[i].meta.codecFields
}

func (fs *fields) getDeltaFields(name string, withSuffix bool) []metrics_codec.Field {
	if !withSuffix {
		return defaultDeltaFields
	}

	i := fs.indexOf(name, DeltaCounterType)
	if i == -1 {
		return nil
	}

	return fs.fields[i].meta.codecFields
}

func (fs *fields) contains(fieldName string, mtype mType) bool {
	if indices, ok := fs.index[fieldName]; ok {
		if len(indices) > 1 {
			if fs.fields[indices[0]].Type == mtype {
				return true
			}
		}
	}
	return false
}

func (fs *fields) indexOf(fieldName string, mtype mType) int {
	if indices, ok := fs.index[fieldName]; ok {
		if len(indices) > 1 {
			if fs.fields[indices[0]].Type == mtype {
				return indices[0]
			}
		}
	}
	return -1
}

func (fs *fields) indexOfActualField(fieldName string, mtype mType) int {
	if indices, ok := fs.index[fieldName]; ok {
		if len(indices) > 1 {
			if fs.fields[indices[0]].Type == mtype {
				return indices[1]
			}
		}
	}
	return -1
}

type Field struct {
	Name    string
	Type    mType
	Buckets []float64
	meta    metaFields
}

func (f *Field) getBucket() []float64 {
	return f.meta.buckets
}

type metaFields struct {
	buckets     []float64 // extra information like buckets
	codecFields []metrics_codec.Field
}

type metaField struct {
	Name      string
	Type      mType
	valueType int8
}

func checkBuckets(buckets []float64) ([]float64, error) {
	if buckets == nil {
		return nil, fmt.Errorf("nil buckets, please check the configuration")
	}

	if len(buckets) == 0 {
		return nil, fmt.Errorf("buckets needs a positive count")
	}

	if len(buckets) > bucketMaxCount {
		return nil, fmt.Errorf("too many buckets: %d, exceeded limit %d", len(buckets), bucketMaxCount)
	}

	for i, lowerBound := range buckets {
		if i < len(buckets)-1 {
			if lowerBound >= buckets[i+1] {
				return nil, fmt.Errorf(
					"histogram buckets must be in increasing order: %f >= %f",
					lowerBound, buckets[i+1])
			}
		} else {
			if math.IsInf(lowerBound, +1) {
				// The +Inf bucket is implicit. Remove it here.
				buckets = buckets[:i]
			}
		}
	}

	return buckets, nil
}
