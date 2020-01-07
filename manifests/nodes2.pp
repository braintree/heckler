File {
  backup => false,
}

node default {
  include $facts['hostname']
}


class fozzie {
  include muppetshow

  file { "/data/puppet_apply/fozzie":
    ensure => directory,
  }
  file { "/data/puppet_apply/fozzie/slapstick":
    ensure => absent,
  }
  service { 'nginx':
    ensure => stopped,
  }
  file { "/data/puppet_apply/fozzie/styx":
    ensure => present,
    content => "",
  }
}

class statler {
  include muppetshow

  file { "/data/puppet_apply/statler":
    ensure => directory,
  }

  file { "/data/puppet_apply/statler/wit":
    ensure => present,
    content => "foul\n",
  }
  service { 'nginx':
    ensure => running,
  }
}

class waldorf {
  include muppetshow

  file { "/data/puppet_apply/waldorf":
    ensure => directory,
  }
  file { "/data/puppet_apply/waldorf/poignant":
    ensure => present,
    content => "sour\n",
  }
  service { 'nginx':
    ensure => running,
  }
}
