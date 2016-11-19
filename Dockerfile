FROM scratch

ADD /lb_collector /

ENTRYPOINT ["/lb_collector"]