File {
  backup => false,
}

node default {
  include $facts['hostname']
}


class fozzie {
  include muppetshow
}

class statler {
  include muppetshow
}

class waldorf {
  include muppetshow
}

class muppetshow {
  file { "/data/puppet_apply":
    ensure => absent,
    force => true,
  }
  file { "/var/www/html/index.html":
    ensure => absent,
    force => true,
  }
  package {[
    'nginx',
    'sl',
    'bsdgames',
  ]:
    ensure => purged,
  }
  user { "kermit":
    ensure     => absent,
  }
  group { "muppets":
    ensure     => absent,
  }
}
