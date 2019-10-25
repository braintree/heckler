File {
  backup => false,
}

node default {
  include $facts['nodename']
}


class fozzie {
  include muppets
}

class statler {
  include muppets
}

class waldorf {
  include muppets
}

class muppets {
  file { "${facts['cwd']}/nodes":
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
