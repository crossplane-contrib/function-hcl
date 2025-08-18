locals {
  baseName       = req.composite.metadata.name
  suffix         = "-primary-bucket"
  name           = "${baseName}${suffix}"
  secondary_name = "${baseName}-secondary-bucket"
  random_ref     = req["resource"]["primary-bucket"].status.id
}

function makeName {
  arg base {}
  arg suffix {
    default = "xxx"
  }
  locals {
    out = "${base}-${suffix}"
  }
  body = out
}

resource primary-bucket {
  body = {
    apiVersion : "aws.com/v1"
    kind : "S3Bucket"
    metadata : {
      name : invoke("makeName", { base = name, suffix = suffix})
    }
    spec : {
      forProvider : {
        region = "us-east-1"
      }
      providerConfigRef : {
        name = "aws-default"
      }
    }
  }

  composite status {
    body = {
      first_ready    = req.resource.primary-bucket.status.ready
      x-primary-size = req.resource.primary-bucket.status.bucket_size
    }
  }

  composite status {
    body = {
      primary-size = self.resource.status.bucket_size
    }
  }
}

resource secondary-bucket {
  body = {
    apiVersion = "aws.com/v1"
    kind       = "S3Bucket"
    metadata = {
      name = secondary_name
    }
    spec = {
      forProvider = {
        region = "us-east-1"
      }
      providerConfigRef = {
        name = "aws-default"
      }
    }
  }

  composite status {
    locals {
      ret = true
    }
    body = {
      second_ready = ret
    }
  }
  composite connection {
    body = {
      second_url = "https://example.com/cm2"
    }
  }

  context {
    key   = "foo"
    value = "bar"
  }

  composite status {
    body = {
      second_ready = true
    }
  }

  composite connection {
    body = {
      second_url = "https://example.com/cm2"
    }
  }

  composite status {
    body = {
      secondary-size = self.resource.status.bucket_size
    }
  }

  composite status {
    body = {
      secondary-size2 = req.resource.secondary-bucket.status.bucket_size
    }
  }

}

resource tertiary-bucket {
  condition = !req.context["example.com/testing"]
  body = {
    apiVersion : "aws.com/v1"
    kind : "S3Bucket"
    metadata : {
      name = "three"
    }
    spec : {
      forProvider : {
        region : "us-east-1"
      }
      providerConfigRef = {
        name : "aws-default"
      }
    }
  }
}

resource unready-bucket {
  body = {
    apiVersion : "aws.com/v1"
    kind : "S3Bucket"
    metadata : {
      name : "four"
    }
    spec : {
      forProvider : {
        region : req.resource.primary-bucket.status.region
      }
      providerConfigRef : {
        name = "aws-default"
      }
    }
  }
}

composite status {
  body = {
    all_ready = true
  }
}

composite status {
  body = {
    junk = req.resource.primary-bucket.status.region
  }
}

composite connection {
  body = {
    url = "https://example.com/config-maps"
  }
}

composite connection {
  body = {
    url = base64encode("https://example.com/config-maps")
  }
}

context {
  key   = "example.com/foo"
  value = "bar"
}

