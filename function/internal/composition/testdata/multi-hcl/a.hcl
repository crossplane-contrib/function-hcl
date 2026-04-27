resource cm_a {
  body = {
    apiVersion = "v1"
    kind       = "ConfigMap"
    data       = { key = "value-a" }
  }
}
