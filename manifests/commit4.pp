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
}

class muppets {
  file { "${facts['cwd']}/nodes":
    ensure => directory,
  }
  package { 'nginx':
    ensure => installed,
  }
  file { "/var/www/html/index.html":
    ensure => present,
    content => "Muppets\n",
    notify => Service['nginx'],
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
    | EOF

  file { "${facts['cwd']}/nodes/the_muppet_show":
    ensure => present,
    content => $the_muppet_show,
  }
}
