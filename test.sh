#!/bin/sh

n=0
while true; do
    echo $n
    n=`expr $n + 1`
    sleep 2
done
