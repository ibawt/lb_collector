FROM scratch

ADD /lb-collector /

ENTRYPOINT ["/lb-collector"]