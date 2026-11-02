package service

import (
	"context"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestRegistryEnvironmentRuntimeAndAdmissionFailBeforeSecretReads(t *testing.T) {
	st,r,f,env:=immutableSecretFixture(t)
	ctx:=context.Background()
	for _,tc:=range []struct{id,name string}{
		{"_registries","legacy"},
		{"registry_owned","private"},
		{env.ID," REGISTRY_owned "},
		{env.ID," _REGISTRIES (internal) "},
	} {
		t.Run(tc.id+tc.name,func(t *testing.T){
			x:=&model.Environment{ID:tc.id,Name:tc.name,Version:env.Version}
			if err:=st.Environments.Create(ctx,x);err!=nil{t.Fatal(err)}
			if _,err:=r.ResolveDetailed(ctx,&model.App{EnvironmentID:tc.id});err==nil{t.Fatal("private runtime binding accepted")}
			if _,err:=r.PinEnvironmentSecrets(ctx,tc.id,x.Version,nil,true);err==nil{t.Fatal("private admission binding accepted")}
			if err:=ValidateRuntimeEnvironmentBinding(ctx,st.Environments,tc.id);err==nil{t.Fatal("private draft binding accepted")}
			if f.lists!=0 || f.latestReads!=0 || f.pinnedReads!=0 {t.Fatal("private environment reached provider")}
		})
	}
}
