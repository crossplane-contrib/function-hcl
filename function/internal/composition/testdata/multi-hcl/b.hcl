resource cm_b {
  body = {
    apiVersion = "v1"
    kind       = "ConfigMap"
    data       = { key = "value-b" }
  }
}
