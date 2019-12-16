File {
  backup => false,
}

node default {
  include $facts['nodename']
}


class fozzie {
  include muppetshow

  file { "${facts['cwd']}/nodes/fozzie":
    ensure => directory,
  }
  file { "${facts['cwd']}/nodes/fozzie/slapstick":
    ensure => absent,
  }
  service { 'nginx':
    ensure => stopped,
  }
  file { "/var/www/html/index.html":
    ensure => present,
    content => "Fozzie\n",
  }
  package { 'bsdgames':
    ensure => installed,
  }
  file { "${facts['cwd']}/nodes/fozzie/styx":
    ensure => present,
    content => "",
  }
  exec { 'sail':
    command => '/usr/games/sail -h',
    refreshonly => true,
    subscribe => File["${facts['cwd']}/nodes/fozzie/styx"],
  }
}

class statler {
  include muppetshow

  file { "${facts['cwd']}/nodes/statler":
    ensure => directory,
  }

  file { "${facts['cwd']}/nodes/statler/wit":
    ensure => present,
    content => "foul\n",
  }
  service { 'nginx':
    ensure => running,
  }
  file { "/var/www/html/index.html":
    ensure => present,
    content => "Statler\n",
    notify => Service['nginx'],
  }
  package { 'sl':
    ensure => installed,
  }
}

class waldorf {
  include muppetshow

  file { "${facts['cwd']}/nodes/waldorf":
    ensure => directory,
  }
  file { "${facts['cwd']}/nodes/waldorf/poignant":
    ensure => present,
    content => "sour\n",
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
