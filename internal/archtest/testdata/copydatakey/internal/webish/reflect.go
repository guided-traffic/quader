package webish

import "reflect"

func byName(v any) { _ = reflect.ValueOf(v).MethodByName("CopyDataKey") }
