class muppetshow {
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
    | EOF

  file { "${facts['cwd']}/nodes/the_muppet_show":
    ensure => present,
    content => $the_muppet_show,
  }
  muppetshow::episode { "One":
    base => "${facts['cwd']}/nodes",
  }
  concat { "${facts['cwd']}/nodes/cast":
    ensure => present,
  }
  concat::fragment { 'MissPiggy':
    target  => "${facts['cwd']}/nodes/cast",
    content => "Miss Piggy\n",
    order   => '01'
  }
  concat::fragment { 'RowlfTheDog':
    target  => "${facts['cwd']}/nodes/cast",
    content => "Rowlf the Dog\n",
    order   => '02'
  }
}
