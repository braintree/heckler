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
  file { "/data/puppet_apply/fozzie/manhattan":
    ensure => present,
    content => "Let's take manhattan!\n",
  }
  file { "/var/www/html/index.html":
    ensure => present,
    content => "Fozzie\n",
  }
  package { 'bsdgames':
    ensure => installed,
  }
  file { "/data/puppet_apply/fozzie/styx":
    ensure => present,
    content => "Come Sail Away\n",
  }
  exec { 'sail':
    command => '/usr/games/sail -s',
    refreshonly => true,
    subscribe => File["/data/puppet_apply/fozzie/styx"],
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
  file { "/var/www/html/index.html":
    ensure => present,
    content => "Statler Statler\n",
    notify => Service['nginx'],
  }
  package { 'sl':
    ensure => installed,
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
  file { "/data/puppet_apply/waldorf/manhattan":
    ensure => present,
    content => "Gonzo\n",
  }
  service { 'nginx':
    ensure => running,
  }
  file { "/var/www/html/index.html":
    ensure => present,
    content => "Waldorf\n",
    notify => Service['nginx'],
  }
  package { 'sl':
    ensure => installed,
  }
}
