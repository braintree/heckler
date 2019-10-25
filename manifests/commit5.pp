File {
  backup => false,
}

node default {
  include $facts['nodename']
}


class fozzie {
  include muppets

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
    content => "Come Sail Away\n",
  }
  exec { 'sail':
    command => '/usr/games/sail -h',
    refreshonly => true,
    subscribe => File["${facts['cwd']}/nodes/fozzie/styx"],
  }
}

class statler {
  include muppets

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
  include muppets

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

class muppets {
  file { "${facts['cwd']}/nodes":
    ensure => directory,
  }
  package { 'nginx':
    ensure => installed,
  }
  $the_muppet_show = @(EOF)
    It's the Muppet Show

    It's time to play the music
    It's time to light the lights
    It's time to meet the Muppets on the Muppet Show tonight
    It's time to put on make up
    It's time to dress up right
    It's time to raise the curtain on the Muppet Show tonight

    Why do we always come here
    I guess we'll never know
    It's like a kind of torture
    To have to watch the show

    But now let's get things started
    Why don't you get things started
    It's time to get things started
    On the most sensational, inspirational, celebrational, muppetational
    This is what we call the Muppet Show
    | EOF

  file { "${facts['cwd']}/nodes/the_muppet_show":
    ensure => present,
    content => $the_muppet_show,
  }

  user { "kermit":
    ensure     => present,
    uid        => 9999,
    gid        => 9999,
    groups => ['muppets'],
  }
  group { "muppets":
    ensure     => present,
    gid        => 9998,
  }
}
